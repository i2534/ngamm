package mgr

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ludoux/ngapost2md/nga"
	"github.com/robfig/cron/v3"
	"gopkg.in/ini.v1"
)

var (
	DIR_TOPIC_ROOT  = "."        // 帖子存储根目录, 目前只在工作目录
	DIR_RECYCLE_BIN = "recycles" // 回收站目录
	POST_MARKDOWN   = "post.md"
	PROCESS_INI     = "process.ini"
	METADATA_JSON   = "metadata.json"
	QUEUE_SIZE      = 999
	AUTHOR_ID       = 0
)

type Config struct {
	Addr string
}

type Metadata struct {
	UpdateCron   string
	updateCronId cron.EntryID
}

func (m *Metadata) Merge(n *Metadata) {
	m.UpdateCron = n.UpdateCron
}

type Topic struct {
	Id       int
	Title    string
	Author   string
	Create   time.Time
	MaxPage  int
	MaxFloor int
	Metadata *Metadata
	root     string
}

func LoadTopic(id int) (*Topic, error) {
	dir := nga.FindFolderNameByTid(id, AUTHOR_ID)
	if dir == "" {
		return nil, fmt.Errorf("no folder for topic: %d", id)
	}

	log.Printf("Loading topic %d from %s\n", id, dir)

	md := filepath.Join(dir, POST_MARKDOWN)
	if _, err := os.Stat(md); os.IsNotExist(err) {
		return nil, fmt.Errorf("no %s at dir: %s", POST_MARKDOWN, dir)
	}

	topic := new(Topic)
	topic.root = dir
	topic.Id = id

	re := regexp.MustCompile(`\\<pid:0\\>\s+(\d{4}-\d{2}-\d{2}\s+\d{2}:\d{2}:\d{2})\s+by\s+(.+)\s*<`)
	err := ReadLine(md, func(line string, i int) bool {
		if i == 0 {
			topic.Title = strings.TrimLeft(line, "# ")
		} else {
			m := re.FindStringSubmatch(line)
			if m != nil {
				t, err := time.Parse("2006-01-02 15:04:05", m[1])
				if err != nil {
					log.Println("Failed to parse time:", m[1], err)
					return false
				}
				author := m[2]

				topic.Create = t
				topic.Author = author
				return false
			}
		}
		return true
	})
	if err != nil {
		return nil, err
	}
	if topic.Title == "" {
		return nil, fmt.Errorf("no title in %s at dir: %s", POST_MARKDOWN, dir)
	}

	data, err := ini.Load(filepath.Join(dir, PROCESS_INI))
	if err == nil {
		sec := data.Section("local")
		topic.MaxPage = sec.Key("max_page").MustInt(0)
		topic.MaxFloor = sec.Key("max_floor").MustInt(0)
	}

	meta := new(Metadata)
	td, err := os.ReadFile(filepath.Join(dir, METADATA_JSON))
	if err != nil {
		if os.IsNotExist(err) {
			log.Println("No metadata found for topic", id)
		} else {
			log.Println("Failed to read metadata:", err)
		}
	} else if err := json.Unmarshal(td, meta); err != nil {
		log.Println("Failed to parse metadata:", err)
	}
	topic.Metadata = meta

	return topic, nil
}

func (t *Topic) Save() error {
	dir := t.root
	md := filepath.Join(dir, METADATA_JSON)

	// 将 Metadata 序列化为 JSON
	data, err := json.MarshalIndent(t.Metadata, "", "  ")
	if err != nil {
		return err
	}

	// 将 JSON 数据写入文件
	if err := os.WriteFile(md, data, 0644); err != nil {
		return err
	}
	return nil
}

type cache struct {
	lock *sync.RWMutex
	// topics already loaded
	loaded bool
	topics map[int]*Topic
	// adding
	queue chan int
}

func (c *cache) close() {
	close(c.queue)
}

type Server struct {
	Raw     *http.Server
	Cfg     *Config
	nga     *ClientExt
	stop    chan struct{}
	stopped bool
	lock    *sync.Mutex
	cache   *cache
	cron    *cron.Cron
}

func NewServer(cfg *Config, nga *ClientExt) (*Server, error) {
	// 创建 Gin 路由器
	r := gin.Default()
	as, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		as = time.Local
	}
	srv := &Server{
		Raw: &http.Server{
			Addr:    cfg.Addr,
			Handler: r,
		},
		Cfg:  cfg,
		nga:  nga,
		stop: make(chan struct{}),
		lock: &sync.Mutex{},
		cron: cron.New(cron.WithLocation(as)),
		cache: &cache{
			lock:  &sync.RWMutex{},
			queue: make(chan int, QUEUE_SIZE),
		},
	}
	e := srv.init()
	if e != nil {
		return nil, e
	}
	return srv, nil
}

func (s *Server) init() error {
	go s.loadTopics()

	r := s.Raw.Handler.(*gin.Engine)
	r.GET("/help", func(c *gin.Context) {
		c.String(http.StatusOK, "Hello, this is NGA Post2md Manager")
	})
	tg := r.Group("/topic")
	{
		tg.GET("/", s.topicList())
		tg.GET("/:id", s.topicInfo())
		tg.PUT("/:id", s.topicAdd())
		tg.POST("/:id", s.topicUpdate())
		tg.DELETE("/:id", s.topicDel())
	}

	return nil
}

func (s *Server) loadTopics() {
	c := s.cache
	if c.loaded {
		return
	}

	c.lock.Lock()
	defer c.lock.Unlock()

	if c.loaded {
		return
	}

	files, err := os.ReadDir(DIR_TOPIC_ROOT)
	if err != nil {
		log.Println("Failed to read topic root dir:", err)
	} else {
		c.topics = make(map[int]*Topic, len(files))
		for _, file := range files {
			if !file.IsDir() {
				continue
			}
			fn := file.Name()
			id, err := strconv.Atoi(fn)
			if err != nil {
				log.Println("It's not topic dir:", fn)
				continue
			}

			topic, err := LoadTopic(id)

			if err != nil {
				log.Println("Failed to load topic", err)
				continue
			}

			s.addCron(topic)

			c.topics[id] = topic
		}

		log.Println("Loaded", len(c.topics), "topics")
	}
	c.loaded = true
}

func (s *Server) topicList() func(c *gin.Context) {
	return func(c *gin.Context) {
		cache := s.cache
		cache.lock.RLock()
		defer cache.lock.RUnlock()

		topics := make([]Topic, 0, len(cache.topics))
		for _, topic := range cache.topics {
			topics = append(topics, *topic)
		}

		c.JSON(http.StatusOK, topics)
	}
}

func (s *Server) topicInfo() func(c *gin.Context) {
	return func(c *gin.Context) {
		id, err := strconv.Atoi(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid topic ID"})
			return
		}

		cache := s.cache
		cache.lock.RLock()
		defer cache.lock.RUnlock()

		topic, exists := cache.topics[id]
		if !exists {
			c.JSON(http.StatusNotFound, gin.H{"error": "Topic not found"})
			return
		}

		c.JSON(http.StatusOK, topic)
	}
}

func (s *Server) topicAdd() func(c *gin.Context) {
	return func(c *gin.Context) {
		id, err := strconv.Atoi(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid topic ID"})
			return
		}

		cache := s.cache
		cache.lock.RLock()
		defer cache.lock.RUnlock()

		if _, exists := cache.topics[id]; exists {
			c.JSON(http.StatusConflict, gin.H{"error": "Topic already exists"})
			return
		}

		select {
		case cache.queue <- id:
			c.JSON(http.StatusCreated, id)
			return
		default:
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Too many adding requests"})
			return
		}
	}
}

func (s *Server) topicDel() func(c *gin.Context) {
	return func(c *gin.Context) {
		id, err := strconv.Atoi(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid topic ID"})
			return
		}

		cache := s.cache
		cache.lock.Lock()
		defer cache.lock.Unlock()

		if _, exists := cache.topics[id]; !exists {
			c.JSON(http.StatusNotFound, gin.H{"error": "Topic not found"})
			return
		}

		delete(cache.topics, id)

		go func() {
			dir := nga.FindFolderNameByTid(id, AUTHOR_ID)
			if dir != "" {
				recycles := filepath.Join(DIR_TOPIC_ROOT, DIR_RECYCLE_BIN)
				if err := os.MkdirAll(recycles, 0755); err != nil {
					log.Println("Failed to create recycle bin:", recycles, err)

					log.Println("Removing topic dir:", dir)
					if err := os.RemoveAll(dir); err != nil {
						log.Println("Failed to remove topic dir:", dir, err)
					}
				} else {
					log.Println("Moving topic dir to recycle bin:", dir)
					tar := filepath.Join(recycles, dir)
					os.RemoveAll(tar)
					if err := os.Rename(dir, tar); err != nil {
						log.Println("Failed to move topic dir to recycle bin:", dir, err)
					}
				}
			}
		}()

		c.JSON(http.StatusOK, id)
	}
}

func (s *Server) addCron(topic *Topic) {
	meta := topic.Metadata
	cron := meta.UpdateCron
	if cron != "" {
		log.Println("Adding cron job for topic", topic.Id, ":", cron)
		id, e := s.cron.AddFunc(cron, func() {
			log.Println("Cron job for topic", topic.Id)
			s.cache.queue <- topic.Id
		})
		if e != nil {
			log.Println("Failed to add cron job:", e)
		} else {
			s.cron.Remove(meta.updateCronId)
			meta.updateCronId = id
			// fmt.Println("Cron job id:", id, topic.Metadata.updateCronId)
		}
	}
}

func (s *Server) topicUpdate() func(c *gin.Context) {
	return func(c *gin.Context) {
		id, err := strconv.Atoi(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid topic ID"})
			return
		}

		cache := s.cache

		cache.lock.Lock()
		defer cache.lock.Unlock()

		topic, has := cache.topics[id]
		if !has {
			c.JSON(http.StatusNotFound, gin.H{"error": "Topic not found"})
			return
		}

		var meta Metadata
		if err := c.ShouldBindJSON(&meta); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
			return
		}

		topic.Metadata.Merge(&meta)

		s.addCron(topic)

		go topic.Save()

		c.JSON(http.StatusOK, id)
	}
}

func (s *Server) process() {
	cache := s.cache

	for id := range cache.queue {
		log.Println("Processing topic", id)

		aid := 0
		tie := nga.Tiezi{}
		path := nga.FindFolderNameByTid(id, aid)
		if path != "" {
			tie.InitFromLocal(id, aid)
		} else {
			tie.InitFromWeb(id, aid)
		}
		tie.Download()

		topic, err := LoadTopic(id)
		if err != nil {
			log.Println("Failed to load topic", err)
		} else {
			cache.lock.Lock()
			cache.topics[id] = topic
			cache.lock.Unlock()
		}
	}
}

// 启动服务器并阻塞
func (s *Server) Run() {
	go func() {
		if err := s.Raw.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Listen failed: %s\n", err)
		}
	}()
	log.Println("Server started, listening on", s.Cfg.Addr)

	s.cron.Start()
	go s.process()

	// 等待中断信号以关闭服务器
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt)

	select {
	case <-quit:
		log.Println("Received OS interrupt signal")
	case <-s.stop:
		log.Println("Received stop signal")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.Raw.Shutdown(ctx); err != nil {
		log.Fatal("Server forced to shutdown:", err)
	}
	log.Println("Server exiting")
}

func (s *Server) Stop() {
	s.lock.Lock()
	defer s.lock.Unlock()

	if s.stopped {
		return
	}
	s.stopped = true

	s.cron.Stop()
	s.cache.close()
	close(s.stop)
}
