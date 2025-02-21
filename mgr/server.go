package mgr

import (
	"context"
	"fmt"
	"log"
	"math/rand/v2"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/robfig/cron/v3"
)

var (
	DIR_TOPIC_ROOT    = "."        // 帖子存储根目录, 目前只在工作目录
	DIR_RECYCLE_BIN   = "recycles" // 回收站目录
	POST_MARKDOWN     = "post.md"
	PROCESS_INI       = "process.ini"
	METADATA_JSON     = "metadata.json"
	ASSETSA_JSON      = "assets.json"
	DELETE_FLAG       = "deleted_at"
	DEFAULT_CRON      = "@every 1h"
	DEFAULT_MAX_RETRY = 3
	QUEUE_SIZE        = 9999
	AUTHOR_ID         = 0
	DELETE_TIME       = 7 * 24
	TIME_LOC          = Local()
)

type Config struct {
	Addr      string
	Token     string
	tokenHash string
	Smile     string
	GitHash   string
}

type cache struct {
	lock      *sync.RWMutex
	topicRoot *ExtRoot
	loaded    bool                  // topics already loaded
	topics    *SyncMap[int, *Topic] // all topics
	queue     chan int              // adding or update topic id
	smile     *Smile
}

func (c *cache) Close() error {
	c.topics.EAC(func(_ int, topic *Topic) {
		topic.Close()
	})
	close(c.queue)
	return c.topicRoot.Close()
}

type Server struct {
	Raw      *http.Server
	Cfg      *Config
	nga      *Client
	stopped  bool
	stopChan chan struct{}
	stopLock *sync.Mutex
	cache    *cache
	cron     *cron.Cron
}

func customLogFormatter(param gin.LogFormatterParams) string {
	var statusColor, methodColor, resetColor string
	if param.IsOutputColor() {
		statusColor = param.StatusCodeColor()
		methodColor = param.MethodColor()
		resetColor = param.ResetColor()
	}

	if param.Latency > time.Minute {
		param.Latency = param.Latency.Truncate(time.Second)
	}

	return fmt.Sprintf("%s%s %-7s %s |%s %3d %s| %13v | %15s | %#v\n%s",
		formatTimestamp(param.TimeStamp),
		methodColor, param.Method, resetColor,
		statusColor, param.StatusCode, resetColor,
		param.Latency,
		param.ClientIP,
		param.Path,
		param.ErrorMessage,
	)
}

func formatTimestamp(timestamp time.Time) string {
	flags := log.Flags()
	switch {
	case flags&log.Ldate != 0 && flags&log.Ltime != 0:
		return timestamp.Format("2006/01/02 15:04:05 ")
	case flags&log.Ldate != 0:
		return timestamp.Format("2006/01/02 ")
	case flags&log.Ltime != 0:
		return timestamp.Format("15:04:05 ")
	default:
		return ""
	}
}

func NewServer(cfg *Config, nga *Client) (*Server, error) {
	tr, e := nga.GetRoot().OpenRoot(DIR_TOPIC_ROOT)
	if e != nil {
		return nil, e
	}

	engine := gin.New()
	engine.Use(gin.LoggerWithFormatter(customLogFormatter), gin.Recovery())

	srv := &Server{
		Raw: &http.Server{
			Addr:    cfg.Addr,
			Handler: engine,
		},
		Cfg:      cfg,
		nga:      nga,
		stopChan: make(chan struct{}),
		stopLock: &sync.Mutex{},
		cron:     cron.New(cron.WithLocation(TIME_LOC)),
		cache: &cache{
			lock:      &sync.RWMutex{},
			queue:     make(chan int, QUEUE_SIZE),
			topicRoot: &ExtRoot{tr},
		},
	}

	if e := srv.init(engine); e != nil {
		return nil, e
	}
	if cfg.Token != "" {
		cfg.tokenHash = ShortSha1(cfg.Token)
	}

	nga.srv = srv

	return srv, nil
}

func (srv *Server) init(_ *gin.Engine) error {
	go srv.loadTopics()
	srv.regHandlers()
	return nil
}

func (srv *Server) loadTopics() {
	cache := srv.cache
	cache.lock.Lock()
	defer cache.lock.Unlock()
	if cache.loaded {
		return
	}

	files, e := cache.topicRoot.ReadDir()
	if e != nil {
		log.Println("读取帖子根目录失败:", e)
	} else {
		cache.topics = NewSyncMap[int, *Topic]()
		for _, file := range files {
			if !file.IsDir() {
				continue
			}
			name := file.Name()
			id, e := strconv.Atoi(name)
			if e != nil {
				log.Println("不是帖子目录:", name)
				continue
			}

			topic, e := LoadTopic(cache.topicRoot, id)
			if e != nil {
				log.Println("加载帖子失败:", e)
				continue
			}

			cache.topics.Put(id, topic)
			next := srv.addCron(topic)
			// 在第一次更新时间段(必须超过30分钟)的一半内随机更新一次
			if !next.IsZero() {
				d := time.Until(next)
				if d > time.Minute*30 {
					d = time.Duration(rand.Int64N(int64(d) / 2))
					log.Printf("随机更新帖子 <%s> 在 %s 后\n", topic.Title, d)
					timer := time.AfterFunc(d, func() {
						log.Println("随机更新帖子", topic.Title)
						cache.queue <- id
						topic.timers.Delete(d)
					})
					topic.timers.Put(d, timer)
				}
			}
		}

		log.Println("已加载", cache.topics.Size(), "个帖子")
	}
	cache.loaded = true
}

func (srv *Server) checkRecycleBin() {
	log.Println("检查回收站...")
	recycles, e := srv.cache.topicRoot.AbsPath(DIR_RECYCLE_BIN)
	if e != nil {
		log.Println("获取回收站绝对路径失败:", e)
		return
	}
	files, e := os.ReadDir(recycles)
	if e != nil {
		log.Println("读取回收站失败:", e)
		return
	}
	for _, file := range files {
		if !file.IsDir() {
			continue
		}
		name := file.Name()
		tar := filepath.Join(recycles, name)
		data, e := os.ReadFile(filepath.Join(tar, DELETE_FLAG))
		if e != nil {
			log.Println("读取", DELETE_FLAG, "失败:", e)
			continue
		}
		t, e := time.Parse(time.RFC3339, string(data))
		if e != nil {
			log.Println("解析", DELETE_FLAG, "失败:", e)
			continue
		}
		if time.Since(t).Hours() > float64(DELETE_TIME) {
			log.Println("删除回收站帖子", name)
			if e := os.RemoveAll(tar); e != nil {
				log.Println("删除回收站帖子失败:", e)
			}
		}
	}
}

func (srv *Server) addCron(topic *Topic) time.Time {
	md := topic.Metadata
	uc := md.UpdateCron
	if uc != "" {
		log.Printf("为帖子 <%s> 添加定时任务: %s\n", topic.Title, uc)
		id, e := srv.cron.AddFunc(uc, func() {
			log.Println("为帖子添加处理任务", topic.Id)
			srv.cache.queue <- topic.Id
		})
		if e != nil {
			log.Println("添加定时任务失败:", e)
		} else {
			srv.cron.Remove(md.updateCronId)
			md.updateCronId = id
			return srv.cron.Entry(id).Next
		}
	} else {
		srv.cron.Remove(md.updateCronId)
		md.updateCronId = 0
	}
	return time.Time{}
}

func (srv *Server) process() {
	cache := srv.cache
	for id := range cache.queue {
		log.Println("处理帖子", id)

		old, has := cache.topics.Get(id)
		if !has {
			log.Println("未找到帖子", id, "，是否已删除？")
			continue
		}

		log.Printf("更新帖子 %d <%s>\n", id, old.Title)

		// 先检查 process.ini, assets.json 存在与否, 如果文件夹存在但文件不存在, ngapost2md 会认为其是无效的帖子, 不予更新
		dir := old.root
		// 创建文件夹, 防止因为异步导致文件夹在判断 process.ini, assets.json 之后被创建, 然后导致 ngapost2md 无法更新
		if !dir.IsExist(PROCESS_INI) {
			data := `[local]
max_page = 1
max_floor = -1`
			if e := dir.WriteAll(PROCESS_INI, []byte(data)); e != nil {
				log.Println("创建 process.ini 失败:", e)
			}
		}
		if !dir.IsExist(ASSETSA_JSON) {
			data := "{}"
			if e := dir.WriteAll(ASSETSA_JSON, []byte(data)); e != nil {
				log.Println("创建 assets.json 失败:", e)
			}
		}

		ok, msg := srv.nga.DownTopic(id)
		if ok {
			topic, e := LoadTopic(cache.topicRoot, id)
			if e != nil {
				log.Println("加载帖子失败:", e)
			} else {
				topic.Result = DownResult{
					Success: true,
					Time:    Now(),
				}
				topic.Metadata.retryCount = 0

				cache.topics.Put(id, topic)
			}
		} else {
			if topic, has := cache.topics.Get(id); has {
				topic.Result = DownResult{
					Success: false,
					Message: msg,
					Time:    Now(),
				}
				topic.Modify()

				md := topic.Metadata
				if md.MaxRetryCount >= 0 {
					mrc := md.MaxRetryCount
					if mrc == 0 {
						mrc = DEFAULT_MAX_RETRY
					}
					md.retryCount += 1
					log.Println("失败次数:", md.retryCount)
					if md.retryCount >= mrc {
						log.Printf("帖子失败次数 %d 达到最大重试次数 (%d)\n", id, md.retryCount)
						srv.cron.Remove(md.updateCronId)
						md.updateCronId = 0
					}
				}
			}
		}
	}
}

func (srv *Server) getTopics(username string) []*Topic {
	cache := srv.cache
	cache.lock.RLock()
	defer cache.lock.RUnlock()

	ret := make([]*Topic, 0)
	for _, topic := range cache.topics.Values() {
		if topic.Author == username {
			ret = append(ret, topic)
		}
	}
	return ret
}

// 启动服务器并阻塞
func (srv *Server) Run() {
	go func() {
		if e := srv.Raw.ListenAndServe(); e != nil && e != http.ErrServerClosed {
			log.Fatalf("监听失败: %s\n", e)
		}
	}()
	log.Println("服务器已启动，监听端口", srv.Cfg.Addr)

	srv.cron.AddFunc("@every 12h", srv.checkRecycleBin)
	srv.cron.Start()

	go srv.process()

	// 等待中断信号以关闭服务器
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt)

	select {
	case <-quit:
		log.Println("收到操作系统中断信号")
	case <-srv.stopChan:
		log.Println("收到停止信号")
	}

	srv.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if e := srv.Raw.Shutdown(ctx); e != nil {
		log.Fatal("服务器强制关闭:", e)
	}
	log.Println("服务器已退出")
}

func (srv *Server) Stop() {
	log.Println("正在停止服务器...")

	srv.stopLock.Lock()
	defer srv.stopLock.Unlock()

	if srv.stopped {
		return
	}
	srv.stopped = true

	srv.cron.Stop()
	srv.cache.Close()
	srv.nga.Close()

	close(srv.stopChan)

	log.Println("服务器已停止")
}
