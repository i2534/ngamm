package mgr

import (
	"embed"
	"html/template"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
	"github.com/robfig/cron/v3"
)

const (
	ERR_KEY     = "error"
	HTML_HEADER = "text/html; charset=utf-8"
	SMILE_DIR   = "smile"
	ATTACH_DIR  = "attachments"
)

func (srv *Server) regHandlers() {
	has := srv.Cfg.Token != ""

	r := srv.Raw.Handler.(*gin.Engine)
	r.Use(gzip.Gzip(gzip.DefaultCompression))

	tg := r.Group("/topic")
	{
		if has {
			tg.Use(srv.topicMiddleware())
		}
		tg.GET("", srv.topicList())
		tg.GET("/", srv.topicList())
		tg.GET("/:id", srv.topicInfo())
		tg.PUT("/:id", srv.topicAdd())
		tg.POST("/:id", srv.topicUpdate())
		tg.DELETE("/:id", srv.topicDel())
		tg.POST("/fresh/:id", srv.topicFresh())
	}

	vg := r.Group("/view")
	{
		if has {
			vg.Use(srv.viewMiddleware())
		}
		vg.GET("/:token/:id", srv.viewTopic())
		vg.GET("/:token/:id/:name", srv.viewTopicRes())
	}

	r.GET("/", srv.homePage())
	r.GET("/favicon.ico", srv.favicon())
	r.GET("/asset/:name", srv.asset())
}

func toErr(msg string) gin.H {
	return gin.H{ERR_KEY: msg}
}

func (srv *Server) topicMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := c.GetHeader("Authorization")
		if token != srv.Cfg.Token {
			c.JSON(http.StatusUnauthorized, toErr("Unauthorized"))
			c.Abort()
			return
		}
		c.Next()
	}
}

func (srv *Server) viewMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := c.Param("token")
		if token != srv.Cfg.tokenHash {
			c.JSON(http.StatusUnauthorized, toErr("Unauthorized"))
			c.Abort()
			return
		}
		c.Next()
	}
}

func (srv *Server) topicList() func(c *gin.Context) {
	return func(c *gin.Context) {
		cache := srv.cache
		cache.lock.RLock()
		defer cache.lock.RUnlock()

		topics := make([]Topic, 0, len(cache.topics))
		for _, topic := range cache.topics {
			topics = append(topics, *topic)
		}

		c.JSON(http.StatusOK, topics)
	}
}

func (srv *Server) topicInfo() func(c *gin.Context) {
	return func(c *gin.Context) {
		id, e := strconv.Atoi(c.Param("id"))
		if e != nil {
			c.JSON(http.StatusBadRequest, toErr("Invalid topic ID"))
			return
		}

		cache := srv.cache
		cache.lock.RLock()
		defer cache.lock.RUnlock()

		topic, has := cache.topics[id]
		if !has {
			c.JSON(http.StatusNotFound, toErr("Topic not found"))
			return
		}

		c.JSON(http.StatusOK, topic)
	}
}

func (srv *Server) topicAdd() func(c *gin.Context) {
	return func(c *gin.Context) {
		id, e := strconv.Atoi(c.Param("id"))
		if e != nil {
			c.JSON(http.StatusBadRequest, toErr("Invalid topic ID"))
			return
		}

		cache := srv.cache
		lock := cache.lock
		lock.RLock()
		if _, has := cache.topics[id]; has {
			lock.RUnlock()
			c.JSON(http.StatusConflict, toErr("Topic already exists"))
			return
		}
		lock.RUnlock()

		select {
		case cache.queue <- id:
			lock.Lock()
			defer lock.Unlock()

			topic := &Topic{
				root:   filepath.Join(cache.topicRoot, strconv.Itoa(id)),
				Id:     id,
				Create: Now(),
				Metadata: &Metadata{
					UpdateCron: DEFAULT_CRON,
				},
			}
			cache.topics[id] = topic
			go topic.Save()

			c.JSON(http.StatusCreated, id)
		default:
			c.JSON(http.StatusServiceUnavailable, toErr("Too many adding requests"))
		}
	}
}

func (srv *Server) topicDel() func(c *gin.Context) {
	return func(c *gin.Context) {
		id, e := strconv.Atoi(c.Param("id"))
		if e != nil {
			c.JSON(http.StatusBadRequest, toErr("Invalid topic ID"))
			return
		}

		cache := srv.cache
		cache.lock.Lock()
		defer cache.lock.Unlock()

		topic, has := cache.topics[id]
		if !has {
			c.JSON(http.StatusNotFound, toErr("Topic not found"))
			return
		}

		delete(cache.topics, id)

		go func() {
			log.Println("Delete topic", id)
			dir := topic.root
			if dir != "" {
				recycles := filepath.Join(cache.topicRoot, DIR_RECYCLE_BIN)
				if e := os.MkdirAll(recycles, 0755); e != nil {
					log.Println("Failed to create recycle bin:", recycles, e)

					log.Println("Remove topic dir:", dir)
					if e := os.RemoveAll(dir); e != nil {
						log.Println("Failed to remove topic dir:", dir, e)
					}
				} else {
					log.Println("Moving topic dir to recycle bin:", dir)
					tar := filepath.Join(recycles, strconv.Itoa(id))
					os.RemoveAll(tar) // remove old
					if e := os.Rename(dir, tar); e != nil {
						log.Println("Failed to move topic dir to recycle bin:", dir, e)
					} else {
						os.WriteFile(filepath.Join(tar, DELETE_FLAG), []byte(time.Now().Format(time.RFC3339)), 0644)
					}
				}
			} else {
				log.Println("No topic dir to delete")
			}
		}()

		c.JSON(http.StatusOK, id)
	}
}

func (srv *Server) topicUpdate() func(c *gin.Context) {
	return func(c *gin.Context) {
		id, e := strconv.Atoi(c.Param("id"))
		if e != nil {
			c.JSON(http.StatusBadRequest, toErr("Invalid topic ID"))
			return
		}

		cache := srv.cache
		cache.lock.Lock()
		defer cache.lock.Unlock()

		topic, has := cache.topics[id]
		if !has {
			c.JSON(http.StatusNotFound, toErr("Topic not found"))
			return
		}

		md := new(Metadata)
		if err := c.ShouldBindJSON(md); err != nil {
			c.JSON(http.StatusBadRequest, toErr("Invalid request body"))
			return
		}
		topic.Metadata.Merge(md)

		uc := topic.Metadata.UpdateCron
		if uc != "" {
			_, err := cron.ParseStandard(uc)
			if err != nil {
				c.JSON(http.StatusBadRequest, toErr("Invalid cron expression"))
				return
			}
		}

		srv.addCron(topic)
		go topic.Save()

		c.JSON(http.StatusOK, id)
	}
}

func (srv *Server) topicFresh() func(c *gin.Context) {
	return func(c *gin.Context) {
		id, e := strconv.Atoi(c.Param("id"))
		if e != nil {
			c.JSON(http.StatusBadRequest, toErr("Invalid topic ID"))
			return
		}

		cache := srv.cache
		cache.lock.RLock()
		defer cache.lock.RUnlock()

		_, has := cache.topics[id]
		if !has {
			c.JSON(http.StatusNotFound, toErr("Topic not found"))
			return
		}

		select {
		case cache.queue <- id:
			c.JSON(http.StatusOK, id)
		default:
			c.JSON(http.StatusServiceUnavailable, toErr("Too many adding requests"))
		}
	}
}

//go:embed assets/*
var efs embed.FS

func (srv *Server) viewTopicRes() func(c *gin.Context) {
	return func(c *gin.Context) {
		tid := c.Param("id")

		name := c.Param("name")
		if name == "" {
			c.String(http.StatusBadRequest, "Invalid file name")
			return
		}

		if tid == "smile" { // 处理使用默认表情设置的情况
			srv.replaySmile(c, name)
			return
		}

		cache := srv.cache
		id, e := strconv.Atoi(tid)
		if e != nil {
			c.String(http.StatusBadRequest, "Invalid topic ID")
			return
		}

		cache.lock.RLock()
		defer cache.lock.RUnlock()

		topic, has := cache.topics[id]
		if !has {
			c.String(http.StatusNotFound, "Topic not found")
			return
		}

		if strings.HasPrefix(name, "at_") { // load attachment failed from NGA
			srv.replayAttachment(c, name[3:], topic)
			return
		}

		// 确保文件路径在 root 目录下, 防止路径穿越
		file := filepath.Join(topic.root, filepath.Clean(name))
		if !strings.HasPrefix(file, topic.root) {
			c.String(http.StatusBadRequest, "Illegal file name")
			return
		}

		data, e := os.ReadFile(file)
		if e != nil {
			c.String(http.StatusInternalServerError, "Failed to read asset")
			return
		}
		c.Data(http.StatusOK, ContentType(name), data)
	}
}

func (srv *Server) replayAttachment(c *gin.Context, name string, topic *Topic) {
	i := strings.IndexByte(name, '_')
	if i < 0 {
		c.String(http.StatusBadRequest, "Invalid attachment name, missing floor")
		return
	}
	src, e := url.QueryUnescape(name[i+1:])
	if e != nil {
		c.String(http.StatusBadRequest, "Invalid attachment name, failed to unescape")
		return
	}
	ext := filepath.Ext(src)
	dir := filepath.Join(topic.root, ATTACH_DIR)
	os.MkdirAll(dir, os.ModePerm)
	fn := name[:i] + "_" + ShortSha1(src) + ext
	file := filepath.Join(dir, fn)
	if IsExist(file) {
		f, e := os.Open(file)
		if e != nil {
			c.String(http.StatusInternalServerError, "Failed to open attachment file")
			return
		}
		defer f.Close()

		log.Println("Hit attachment", fn)

		c.Header("Content-Type", ContentType(fn))
		if _, e := io.Copy(c.Writer, f); e != nil {
			c.String(http.StatusInternalServerError, "Failed to copy attachment content")
		}
		return
	}

	if strings.Contains(src, ".nga.178.com/attachments/") {
		req, e := http.NewRequest(http.MethodGet, src, nil)
		if e != nil {
			c.String(http.StatusInternalServerError, "Invalid attachment name, failed to create request")
			return
		}
		req.Header.Set("User-Agent", srv.nga.GetUA())

		resp, e := srv.hc.Do(req)
		if e != nil {
			c.String(http.StatusInternalServerError, "Failed to fetch attachment")
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			c.String(http.StatusInternalServerError, "Failed to fetch attachment, status code "+strconv.Itoa(resp.StatusCode))
			return
		}

		f, e := os.Create(file)
		if e != nil {
			c.String(http.StatusInternalServerError, "Failed to open attachment file")
			return
		}
		defer f.Close()

		c.Header("Content-Type", resp.Header.Get("Content-Type"))
		c.Header("Content-Length", resp.Header.Get("Content-Length"))
		iw := io.MultiWriter(c.Writer, f)
		if _, e := io.Copy(iw, resp.Body); e != nil {
			c.String(http.StatusInternalServerError, "Failed to copy attachment content")
		}
	} else {
		c.String(http.StatusBadRequest, "Invalid attachment name, it's not NGA's attachment")
	}
}

func (srv *Server) replaySmile(c *gin.Context, name string) {
	cache := srv.cache
	if cache.smile == nil {
		cache.lock.Lock()
		defer cache.lock.Unlock()

		data, e := efs.ReadFile("assets/smiles.json")
		if e != nil {
			c.String(http.StatusInternalServerError, "Failed to load embed smiles.json")
			return
		}
		if smile, e := Unmarshal(data); e != nil {
			c.String(http.StatusInternalServerError, "Failed to parse embed smiles.json")
			return
		} else {
			smile.root = filepath.Clean(filepath.Join(cache.topicRoot, SMILE_DIR))
			cache.smile = smile
		}
	}

	if srv.Cfg.Smile == "web" {
		url := cache.smile.URL(name)
		if url == "" {
			log.Printf("Smile %s not found\n", name)
			c.String(http.StatusNotFound, "Smile "+name+" not found")
		} else {
			c.Redirect(http.StatusMovedPermanently, url)
		}
	} else {
		path := cache.smile.Local(name, srv.nga.GetUA())
		if path == "" {
			log.Printf("Smile %s not found\n", name)
			c.String(http.StatusNotFound, "Smile "+name+" not found")
		} else {
			data, e := os.ReadFile(path)
			if e != nil {
				c.String(http.StatusInternalServerError, "Failed to read smile")
				return
			}
			c.Data(http.StatusOK, ContentType(path), data)
		}
	}
}

type viewTplData struct {
	Title    string
	ID       int
	Token    string
	BaseUrl  string
	Markdown template.HTML
}

func (srv *Server) viewTopic() func(c *gin.Context) {
	return func(c *gin.Context) {
		title, markdown := "", ""

		id, e := strconv.Atoi(c.Param("id"))
		if e != nil {
			title = "Invalid topic ID"
		} else {
			cache := srv.cache
			cache.lock.RLock()
			defer cache.lock.RUnlock()

			topic, has := cache.topics[id]
			if !has {
				title = "Topic not found"
			} else {
				title = topic.Title
				markdown, e = topic.Content()
				if e != nil {
					title = "Failed to read topic"
				} else {
					markdown += "----\n"
				}
			}
		}

		tmpl, e := template.ParseFS(efs, "assets/view.html")
		if e != nil {
			c.String(http.StatusInternalServerError, "Failed to load view page")
			return
		}

		token := srv.Cfg.tokenHash
		if token == "" {
			token = "-"
		}
		data := viewTplData{
			Title:    title,
			ID:       id,
			Token:    token,
			BaseUrl:  srv.nga.BaseURL(),
			Markdown: template.HTML(markdown),
		}
		c.Header("Content-Type", HTML_HEADER)
		if e := tmpl.Execute(c.Writer, data); e != nil {
			c.String(http.StatusInternalServerError, "Failed to render view page")
		}
	}
}

func (srv *Server) homePage() func(c *gin.Context) {
	return func(c *gin.Context) {
		tmpl, e := template.ParseFS(efs, "assets/home.html")
		if e != nil {
			c.String(http.StatusInternalServerError, "Failed to load home page")
			return
		}
		data := struct {
			HasToken bool
			BaseUrl  string
		}{
			HasToken: srv.Cfg.Token != "",
			BaseUrl:  srv.nga.BaseURL(),
		}
		c.Header("Content-Type", HTML_HEADER)
		if e := tmpl.Execute(c.Writer, data); e != nil {
			c.String(http.StatusInternalServerError, "Failed to render view page")
		}
	}
}

func (srv *Server) favicon() func(c *gin.Context) {
	return func(c *gin.Context) {
		data, err := efs.ReadFile("assets/favicon.ico")
		if err != nil {
			c.String(http.StatusInternalServerError, "Failed to read favicon.ico")
			return
		}
		c.Data(http.StatusOK, "image/x-icon", data)
	}
}

func (srv *Server) asset() func(c *gin.Context) {
	return func(c *gin.Context) {
		name := c.Param("name")
		data, err := efs.ReadFile("assets/" + name)
		if err != nil {
			c.String(http.StatusInternalServerError, "Failed to read asset "+name)
			return
		}
		c.Data(http.StatusOK, ContentType(name), data)
	}
}
