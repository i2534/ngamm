package mgr

import (
	"context"
	"crypto/sha1"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
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
	"github.com/robfig/cron/v3"
	"gopkg.in/ini.v1"
)

func local() *time.Location {
	as, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		as = time.Local
	}
	return as
}

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
	TIME_LOC        = local()
)

type Config struct {
	Addr      string
	Token     string
	tokenHash string
}

type Metadata struct {
	UpdateCron    string
	updateCronId  cron.EntryID
	MaxRetryCount int
	retryCount    int
}

func (m *Metadata) Merge(n *Metadata) {
	m.UpdateCron = n.UpdateCron
	m.MaxRetryCount = n.MaxRetryCount
}

type CustomTime struct {
	time.Time
}

func (t CustomTime) MarshalJSON() ([]byte, error) {
	if t.IsZero() {
		return []byte(`""`), nil
	}
	lt := t.In(TIME_LOC)
	return json.Marshal(lt.Format("2006-01-02 15:04:05"))
}
func fromTime(t time.Time) CustomTime {
	return CustomTime{Time: t}
}
func now() CustomTime {
	return fromTime(time.Now())
}

type Result struct {
	Success bool
	Message string
	Time    CustomTime
}

type Topic struct {
	root     string
	Id       int
	Title    string
	Author   string
	Create   CustomTime
	MaxPage  int
	MaxFloor int
	Metadata *Metadata
	Result   Result
	// assets   map[string]string
}

func isExist(path string) bool {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return false
	}
	return true
}

func LoadTopic(root string, id int) (*Topic, error) {
	dir := filepath.Join(root, strconv.Itoa(id))
	log.Printf("Loading topic %d from %s\n", id, dir)

	md := filepath.Join(dir, POST_MARKDOWN)
	if !isExist(md) {
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

				topic.Create = fromTime(t)
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

	// assets := make(map[string]string)
	// af, err := os.ReadFile(filepath.Join(dir, ASSETSA_JSON))
	// if err != nil {
	// 	if os.IsNotExist(err) {
	// 		log.Println("No assets found for topic", id)
	// 	} else {
	// 		log.Println("Failed to read assets:", err)
	// 	}
	// } else if err := json.Unmarshal(af, &assets); err != nil {
	// 	log.Println("Failed to parse assets:", err)
	// }
	// topic.assets = assets

	return topic, nil
}

func (t *Topic) Save() error {
	dir := t.root

	os.MkdirAll(dir, 0755)

	md := filepath.Join(dir, METADATA_JSON)

	// 将 Metadata 序列化为 JSON
	data, err := json.MarshalIndent(t.Metadata, "", "  ")
	if err != nil {
		return err
	}

	// 将 JSON 数据写入文件
	return os.WriteFile(md, data, 0644)
}

func (t *Topic) Read() (string, error) {
	md := filepath.Join(t.root, POST_MARKDOWN)
	data, e := os.ReadFile(md)
	if e != nil {
		return "", e
	}
	return string(data), nil
}

type Smile struct {
	Base string `json:"base"`
	List []struct {
		Name   string `json:"name"`
		Path   string `json:"path"`
		Prefix string `json:"prefix"`
	} `json:"list"`

	cache map[string]string
}

func (s *Smile) Origin(name string) string {
	n := name[:strings.LastIndex(name, ".")]
	if v, ok := s.cache[n]; ok {
		return v
	}
	for _, v := range s.List {
		if strings.HasPrefix(n, v.Prefix) && strings.HasSuffix(n, v.Name) {
			r := s.Base + v.Path
			s.cache[n] = r
			return r
		}
	}
	return ""
}

type cache struct {
	lock *sync.RWMutex
	// topics already loaded
	loaded bool
	topics map[int]*Topic
	// adding
	queue chan int
	smile *Smile
}

func (c *cache) close() {
	close(c.queue)
}

type Server struct {
	Raw       *http.Server
	Cfg       *Config
	nga       *Client
	stop      chan struct{}
	stopped   bool
	lock      *sync.Mutex
	cache     *cache
	cron      *cron.Cron
	topicRoot string
}

func NewServer(cfg *Config, nga *Client) (*Server, error) {
	// 创建 Gin 路由器
	r := gin.Default()
	srv := &Server{
		Raw: &http.Server{
			Addr:    cfg.Addr,
			Handler: r,
		},
		Cfg:  cfg,
		nga:  nga,
		stop: make(chan struct{}),
		lock: &sync.Mutex{},
		cron: cron.New(cron.WithLocation(TIME_LOC)),
		cache: &cache{
			lock:  &sync.RWMutex{},
			queue: make(chan int, QUEUE_SIZE),
		},
		topicRoot: filepath.Join(nga.GetRoot(), DIR_TOPIC_ROOT),
	}
	e := srv.init()
	if e != nil {
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

func (s *Server) init() error {
	go s.loadTopics()

	r := s.Raw.Handler.(*gin.Engine)

	tg := r.Group("/topic")
	{
		if s.Cfg.Token != "" {
			tg.Use(s.topicMiddleware())
		}
		tg.GET("", s.topicList())
		tg.GET("/", s.topicList())
		tg.GET("/:id", s.topicInfo())
		tg.PUT("/:id", s.topicAdd())
		tg.POST("/:id", s.topicUpdate())
		tg.DELETE("/:id", s.topicDel())
	}
	vg := r.Group("/view")
	{
		if s.Cfg.Token != "" {
			vg.Use(s.viewMiddleware())
		}
		vg.GET("/:token/:id", s.viewTopic())
		vg.GET("/:token/:id/:name", s.viewTopicRes())
	}

	r.GET("/", s.homePage())

	return nil
}

func (s *Server) topicMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := c.GetHeader("Authorization")
		if token != s.Cfg.Token { // 简单的权限校验
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
			c.Abort()
			return
		}
		c.Next()
	}
}

func (s *Server) viewMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := c.Param("token")
		if token != s.Cfg.tokenHash { // 简单的权限校验
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
			c.Abort()
			return
		}
		c.Next()
	}
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

	dir := s.topicRoot
	files, err := os.ReadDir(dir)
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

			topic, err := LoadTopic(dir, id)

			if err != nil {
				log.Println("Failed to load topic:", err)
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

		lock := cache.lock

		lock.RLock()
		if _, exists := cache.topics[id]; exists {
			lock.RUnlock()
			c.JSON(http.StatusConflict, gin.H{"error": "Topic already exists"})
			return
		}
		lock.RUnlock()

		select {
		case cache.queue <- id:
			lock.Lock()
			defer lock.Unlock()

			cache.topics[id] = &Topic{
				root:   filepath.Join(s.topicRoot, strconv.Itoa(id)),
				Id:     id,
				Create: now(),
				Metadata: &Metadata{
					UpdateCron: DEFAULT_CRON,
				},
			}

			c.JSON(http.StatusCreated, id)
			return
		default:
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Too many adding requests"})
			return
		}
	}
}

func (s *Server) checkRecycleBin() {
	log.Println("Checking recycle bin...")
	recycles := filepath.Join(s.topicRoot, DIR_RECYCLE_BIN)
	files, err := os.ReadDir(recycles)
	if err != nil {
		log.Println("Failed to read recycle bin:", err)
		return
	}
	for _, file := range files {
		if !file.IsDir() {
			continue
		}
		name := file.Name()
		tar := filepath.Join(recycles, name)
		data, err := os.ReadFile(filepath.Join(tar, DELETE_FLAG))
		if err != nil {
			log.Println("Failed to read deleted_at:", err)
			continue
		}
		t, err := time.Parse(time.RFC3339, string(data))
		if err != nil {
			log.Println("Failed to parse deleted_at:", err)
			continue
		}
		if time.Since(t).Hours() > float64(DELETE_TIME) {
			log.Println("Removing recycle topic", name)
			if err := os.RemoveAll(tar); err != nil {
				log.Println("Failed to remove recycle topic:", err)
			}
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

		topic, exists := cache.topics[id]
		if !exists {
			c.JSON(http.StatusNotFound, gin.H{"error": "Topic not found"})
			return
		}

		delete(cache.topics, id)

		go func() {
			log.Println("Removing topic", id)
			dir := topic.root
			if dir != "" {
				recycles := filepath.Join(s.topicRoot, DIR_RECYCLE_BIN)
				if err := os.MkdirAll(recycles, 0755); err != nil {
					log.Println("Failed to create recycle bin:", recycles, err)

					log.Println("Removing topic dir:", dir)
					if err := os.RemoveAll(dir); err != nil {
						log.Println("Failed to remove topic dir:", dir, err)
					}
				} else {
					log.Println("Moving topic dir to recycle bin:", dir)
					tar := filepath.Join(recycles, strconv.Itoa(id))
					os.RemoveAll(tar)
					if err := os.Rename(dir, tar); err != nil {
						log.Println("Failed to move topic dir to recycle bin:", dir, err)
					} else {
						os.WriteFile(filepath.Join(tar, DELETE_FLAG), []byte(time.Now().Format(time.RFC3339)), 0644)
					}
				}
			} else {
				log.Println("No topic dir to remove")
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
	} else {
		s.cron.Remove(meta.updateCronId)
		meta.updateCronId = 0
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

		// 先检查 process.ini, assets.json 存在与否, 如果文件夹存在但文件不存在, ngapost2md 会认为其是无效的帖子, 不予更新
		dir := filepath.Join(s.topicRoot, strconv.Itoa(id))
		if isExist(dir) {
			ini := filepath.Join(dir, PROCESS_INI)
			if !isExist(ini) {
				log.Println("No process.ini found for topic, create it...")

				data := `[local]
max_page = 0
max_floor = 0`
				if err := os.WriteFile(ini, []byte(data), 0644); err != nil {
					log.Println("Failed to create process.ini:", err)
				}
			}
			aj := filepath.Join(dir, ASSETSA_JSON)
			if !isExist(aj) {
				log.Println("No assets.json found for topic, create it...")

				data := "{}"
				if err := os.WriteFile(aj, []byte(data), 0644); err != nil {
					log.Println("Failed to create assets.json:", err)
				}
			}
		}

		ok, msg := s.nga.Download(id)
		if ok {
			topic, err := LoadTopic(s.topicRoot, id)
			if err != nil {
				log.Println("Failed to load topic:", err)
			} else {
				topic.Result = Result{
					Success: true,
					Time:    now(),
				}
				topic.Metadata.retryCount = 0

				cache.lock.Lock()
				cache.topics[id] = topic
				cache.lock.Unlock()
			}
		} else {
			cache.lock.Lock()
			topic, exists := cache.topics[id]
			if exists {
				topic.Result = Result{
					Success: false,
					Message: msg,
					Time:    now(),
				}

				md := topic.Metadata
				if md.MaxRetryCount > 0 {
					md.retryCount += 1
					log.Println("Failed count:", md.retryCount)
					if md.retryCount >= md.MaxRetryCount {
						log.Printf("Max retry count reached (%d) for topic %d\n", md.retryCount, id)
						s.cron.Remove(md.updateCronId)
						md.updateCronId = 0
					}
				}
			}
			cache.lock.Unlock()
		}
	}
}

//go:embed assets/*
var efs embed.FS

func (s *Server) viewTopicRes() func(c *gin.Context) {
	return func(c *gin.Context) {
		tid := c.Param("id")

		name := c.Param("name")
		if name == "" {
			c.String(http.StatusBadRequest, "Invalid file name")
			return
		}

		cache := s.cache
		// 处理使用默认表情设置的情况
		if tid == "smile" {
			if cache.smile == nil {
				cache.lock.Lock()
				defer cache.lock.Unlock()

				data, err := efs.ReadFile("assets/smiles.json")
				if err != nil {
					c.String(http.StatusInternalServerError, "Failed to load embed smiles.json")
					return
				}

				smile := &Smile{
					cache: make(map[string]string),
				}
				if err := json.Unmarshal(data, smile); err != nil {
					c.String(http.StatusInternalServerError, "Failed to parse embed smiles.json")
					return
				}
				cache.smile = smile
			}

			url := cache.smile.Origin(name)
			if url == "" {
				log.Printf("Smile %s not found\n", name)
				c.String(http.StatusNotFound, "Smile "+name+" not found")
			} else {
				c.Redirect(http.StatusMovedPermanently, url)
			}
		} else {
			id, err := strconv.Atoi(tid)
			if err != nil {
				c.String(http.StatusBadRequest, "Invalid topic ID")
				return
			}

			cache.lock.RLock()
			defer cache.lock.RUnlock()

			topic, exists := cache.topics[id]
			if !exists {
				c.String(http.StatusNotFound, "Topic not found")
				return
			}
			path := name

			// assets := topic.assets
			// path, exists := assets[name]
			// if !exists {
			// 	path = name
			// }

			dir := topic.root
			file := filepath.Join(dir, path)
			data, err := os.ReadFile(file)
			if err != nil {
				c.String(http.StatusInternalServerError, "Failed to read asset")
				return
			}
			c.Data(http.StatusOK, "application/octet-stream", data)
		}
	}
}

func (s *Server) viewTopic() func(c *gin.Context) {
	return func(c *gin.Context) {
		title, markdown := "", ""

		id, err := strconv.Atoi(c.Param("id"))
		if err != nil {
			title = "Invalid topic ID"
		} else {
			cache := s.cache
			cache.lock.RLock()
			defer cache.lock.RUnlock()

			topic, exists := cache.topics[id]
			if !exists {
				title = "Topic not found"
			} else {
				title = topic.Title
				markdown, err = topic.Read()
				if err != nil {
					title = "Failed to read topic"
				} else {
					markdown += "----\n"
				}
			}
		}

		tmpl, err := template.ParseFS(efs, "assets/view.html")
		if err != nil {
			c.String(http.StatusInternalServerError, "Failed to load view page")
			return
		}

		data := struct {
			Title    string
			ID       int
			Token    string
			Markdown string
		}{
			Title:    title,
			ID:       id,
			Token:    s.Cfg.tokenHash,
			Markdown: markdown,
		}

		c.Header("Content-Type", "text/html; charset=utf-8")
		if err := tmpl.Execute(c.Writer, data); err != nil {
			c.String(http.StatusInternalServerError, "Failed to render view page")
		}
	}
}

func (s *Server) homePage() func(c *gin.Context) {
	return func(c *gin.Context) {
		html, err := efs.ReadFile("assets/home.html")
		if err != nil {
			c.String(http.StatusInternalServerError, "Failed to load home page")
			return
		}
		c.Data(http.StatusOK, "text/html; charset=utf-8", html)
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

	s.cron.AddFunc("@every 12h", s.checkRecycleBin)
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
