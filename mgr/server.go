package mgr

import (
	"context"
	"crypto/sha1"
	"fmt"
	"log"
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
	DIR_TOPIC_ROOT  = "."        // 帖子存储根目录, 目前只在工作目录
	DIR_RECYCLE_BIN = "recycles" // 回收站目录
	POST_MARKDOWN   = "post.md"
	PROCESS_INI     = "process.ini"
	METADATA_JSON   = "metadata.json"
	ASSETSA_JSON    = "assets.json"
	DELETE_FLAG     = "deleted_at"
	DEFAULT_CRON    = "@every 1h"
	QUEUE_SIZE      = 999
	AUTHOR_ID       = 0
	DELETE_TIME     = 7 * 24
	TIME_LOC        = Local()
)

type Config struct {
	Addr      string
	Token     string
	tokenHash string
	Smile     string
}

type cache struct {
	lock      *sync.RWMutex
	topicRoot string
	loaded    bool // topics already loaded
	topics    map[int]*Topic
	queue     chan int // adding or update topic id
	smile     *Smile
}

func (c *cache) close() {
	close(c.queue)
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
			topicRoot: filepath.Join(nga.GetRoot(), DIR_TOPIC_ROOT),
		},
	}

	if e := srv.init(); e != nil {
		return nil, e
	}
	if cfg.Token != "" {
		hash := sha1.Sum([]byte(cfg.Token))
		for i := 0; i < len(hash); i++ {
			if i%5 == 0 {
				cfg.tokenHash += fmt.Sprintf("%02x", hash[i])
			}
		}
	}
	return srv, nil
}

func (srv *Server) init() error {
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

	dir := cache.topicRoot
	files, e := os.ReadDir(dir)
	if e != nil {
		log.Println("Failed to read topic root dir:", e)
	} else {
		cache.topics = make(map[int]*Topic, len(files))
		for _, file := range files {
			if !file.IsDir() {
				continue
			}
			name := file.Name()
			id, e := strconv.Atoi(name)
			if e != nil {
				log.Println("It's not topic dir:", name)
				continue
			}

			topic, e := LoadTopic(dir, id)
			if e != nil {
				log.Println("Failed to load topic:", e)
				continue
			}

			srv.addCron(topic)

			cache.topics[id] = topic
		}

		log.Println("Loaded", len(cache.topics), "topics")
	}
	cache.loaded = true
}

func (srv *Server) checkRecycleBin() {
	log.Println("Checking recycle bin...")
	recycles := filepath.Join(srv.cache.topicRoot, DIR_RECYCLE_BIN)
	files, e := os.ReadDir(recycles)
	if e != nil {
		log.Println("Failed to read recycle bin:", e)
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
			log.Println("Failed to read ", DELETE_FLAG, e)
			continue
		}
		t, e := time.Parse(time.RFC3339, string(data))
		if e != nil {
			log.Println("Failed to parse ", DELETE_FLAG, e)
			continue
		}
		if time.Since(t).Hours() > float64(DELETE_TIME) {
			log.Println("Removing recycle topic", name)
			if e := os.RemoveAll(tar); e != nil {
				log.Println("Failed to remove recycle topic:", e)
			}
		}
	}
}

func (srv *Server) addCron(topic *Topic) {
	md := topic.Metadata
	uc := md.UpdateCron
	if uc != "" {
		log.Println("Adding cron job for topic", topic.Id, ":", uc)
		id, e := srv.cron.AddFunc(uc, func() {
			log.Println("Add process task for topic", topic.Id)
			srv.cache.queue <- topic.Id
		})
		if e != nil {
			log.Println("Failed to add cron job:", e)
		} else {
			srv.cron.Remove(md.updateCronId)
			md.updateCronId = id
		}
	} else {
		srv.cron.Remove(md.updateCronId)
		md.updateCronId = 0
	}
}

func (srv *Server) process() {
	cache := srv.cache
	for id := range cache.queue {
		log.Println("Processing topic", id)
		// 先检查 process.ini, assets.json 存在与否, 如果文件夹存在但文件不存在, ngapost2md 会认为其是无效的帖子, 不予更新
		dir := filepath.Join(cache.topicRoot, strconv.Itoa(id))
		// 创建文件夹, 防止因为异步导致文件夹在判断 process.ini, assets.json 之后被创建, 然后导致 ngapost2md 无法更新
		os.MkdirAll(dir, 0755)

		if IsExist(dir) {
			ini := filepath.Join(dir, PROCESS_INI)
			if !IsExist(ini) {
				log.Println("No process.ini found for topic, create it...")

				data := `[local]
max_page = 1
max_floor = -1`
				if e := os.WriteFile(ini, []byte(data), 0644); e != nil {
					log.Println("Failed to create process.ini:", e)
				}
			}
			aj := filepath.Join(dir, ASSETSA_JSON)
			if !IsExist(aj) {
				log.Println("No assets.json found for topic, create it...")

				data := "{}"
				if e := os.WriteFile(aj, []byte(data), 0644); e != nil {
					log.Println("Failed to create assets.json:", e)
				}
			}
		}

		ok, msg := srv.nga.Download(id)
		if ok {
			topic, e := LoadTopic(cache.topicRoot, id)
			if e != nil {
				log.Println("Failed to load topic:", e)
			} else {
				topic.Result = DownResult{
					Success: true,
					Time:    Now(),
				}
				topic.Metadata.retryCount = 0

				cache.lock.Lock()
				cache.topics[id] = topic
				cache.lock.Unlock()
			}
		} else {
			cache.lock.Lock()
			topic, has := cache.topics[id]
			if has {
				topic.Result = DownResult{
					Success: false,
					Message: msg,
					Time:    Now(),
				}

				md := topic.Metadata
				if md.MaxRetryCount > 0 {
					md.retryCount += 1
					log.Println("Failed count:", md.retryCount)
					if md.retryCount >= md.MaxRetryCount {
						log.Printf("Max retry count reached (%d) for topic %d\n", md.retryCount, id)
						srv.cron.Remove(md.updateCronId)
						md.updateCronId = 0
					}
				}
			}
			cache.lock.Unlock()
		}
	}
}

// 启动服务器并阻塞
func (srv *Server) Run() {
	go func() {
		if e := srv.Raw.ListenAndServe(); e != nil && e != http.ErrServerClosed {
			log.Fatalf("Listen failed: %s\n", e)
		}
	}()
	log.Println("Server started, listening on", srv.Cfg.Addr)

	srv.cron.AddFunc("@every 12h", srv.checkRecycleBin)
	srv.cron.Start()

	go srv.process()

	// 等待中断信号以关闭服务器
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt)

	select {
	case <-quit:
		log.Println("Received OS interrupt signal")
	case <-srv.stopChan:
		log.Println("Received stop signal")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if e := srv.Raw.Shutdown(ctx); e != nil {
		log.Fatal("Server forced to shutdown:", e)
	}
	log.Println("Server exiting")
}

func (srv *Server) Stop() {
	srv.stopLock.Lock()
	defer srv.stopLock.Unlock()

	if srv.stopped {
		return
	}
	srv.stopped = true

	srv.cron.Stop()
	srv.cache.close()
	close(srv.stopChan)
}
