package mgr

import (
	"embed"
	"fmt"
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
	r.Use(func(c *gin.Context) {
		c.Header("Cache-Control", "max-age=604800")
		c.Next()
	})

	tg := r.Group("/topic")
	{
		if has {
			tg.Use(srv.topicMiddleware())
		}
		tg.Use(func(c *gin.Context) {
			c.Header("Cache-Control", "no-cache")
			c.Next()
		})
		tg.GET("", srv.topicList())
		tg.GET("/", srv.topicList())
		tg.GET("/:id", srv.topicInfo())
		tg.PUT("/:id", srv.topicAdd())
		tg.POST("/:id", srv.topicUpdate())
		tg.DELETE("/:id", srv.topicDel())
		tg.POST("/fresh/:id", srv.topicFresh())
	}

	sg := r.Group("/subscribe")
	{
		if has {
			sg.Use(srv.topicMiddleware())
		}
		sg.GET("/:name", srv.subscribeStatus())
		sg.POST("/:name", srv.subscribe())
		sg.DELETE("/:name", srv.unsubscribe())
		sg.POST("/batch", srv.subscribeBatchStatus())
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
			c.JSON(http.StatusUnauthorized, toErr("未授权"))
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
			c.JSON(http.StatusUnauthorized, toErr("未授权"))
			c.Abort()
			return
		}
		c.Next()
	}
}

func (srv *Server) topicList() func(c *gin.Context) {
	return func(c *gin.Context) {
		topics := srv.cache.topics

		ims := c.GetHeader("If-Modified-Since")
		if ims != "" {
			if t, e := time.Parse(time.RFC1123, ims); e == nil {
				ret := make([]*Topic, 0, topics.Size())
				topics.Each(func(_ int, topic *Topic) {
					if topic.modAt.After(t) {
						ret = append(ret, topic)
					}
				})

				c.JSON(http.StatusOK, ret)
				return
			}
		}

		c.JSON(http.StatusOK, topics.Values())
	}
}

func (srv *Server) topicInfo() func(c *gin.Context) {
	return func(c *gin.Context) {
		id, e := strconv.Atoi(c.Param("id"))
		if e != nil {
			c.JSON(http.StatusBadRequest, toErr("无效的帖子 ID"))
			return
		}

		topic, has := srv.cache.topics.Get(id)
		if !has {
			c.JSON(http.StatusNotFound, toErr("未找到帖子"))
			return
		}

		c.JSON(http.StatusOK, topic)
	}
}

func (srv *Server) addTopic(id int) error {
	cache := srv.cache
	if cache.topics.Has(id) {
		return fmt.Errorf("帖子已存在")
	}

	topic := NewTopic(filepath.Join(cache.topicRoot, strconv.Itoa(id)), id)
	topic.Create = Now()
	topic.Metadata.UpdateCron = DEFAULT_CRON

	cache.topics.Put(id, topic)

	select {
	case cache.queue <- id:
		srv.addCron(topic)
		go topic.Save()

		// 刚创建的帖子, 先更新几次, 以便快速获取内容
		intervals := []time.Duration{
			5,
			10,
			15,
			25,
			40,
		}
		for _, interval := range intervals {
			timer := time.AfterFunc(interval*time.Minute, func() {
				srv.cache.queue <- id
				topic.timers.Delete(interval)
			})
			topic.timers.Put(interval, timer)
		}

		log.Println("添加帖子", id)

		return nil
	default:
		return fmt.Errorf("添加请求过多")
	}
}

func (srv *Server) topicAdd() func(c *gin.Context) {
	return func(c *gin.Context) {
		id, e := strconv.Atoi(c.Param("id"))
		if e != nil {
			c.JSON(http.StatusBadRequest, toErr("无效的帖子 ID"))
			return
		}

		e = srv.addTopic(id)
		if e != nil {
			c.JSON(http.StatusConflict, toErr(e.Error()))
		} else {
			c.JSON(http.StatusCreated, id)
		}
	}
}

func (srv *Server) topicDel() func(c *gin.Context) {
	return func(c *gin.Context) {
		id, e := strconv.Atoi(c.Param("id"))
		if e != nil {
			c.JSON(http.StatusBadRequest, toErr("无效的帖子 ID"))
			return
		}

		cache := srv.cache

		topic, has := cache.topics.Get(id)
		if !has {
			c.JSON(http.StatusNotFound, toErr("未找到帖子"))
			return
		}

		cache.topics.Delete(id)

		go func() {
			log.Println("删除帖子", id)
			topic.Stop()
			dir := topic.root
			if dir != "" {
				recycles := filepath.Join(cache.topicRoot, DIR_RECYCLE_BIN)
				if e := os.MkdirAll(recycles, 0755); e != nil {
					log.Println("创建回收站失败:", recycles, e)

					log.Println("删除帖子:", dir)
					if e := os.RemoveAll(dir); e != nil {
						log.Println("删除帖子失败:", dir, e)
					}
				} else {
					log.Println("移动帖子到回收站:", dir)
					tar := filepath.Join(recycles, strconv.Itoa(id))
					os.RemoveAll(tar) // remove old
					if e := os.Rename(dir, tar); e != nil {
						log.Println("移动帖子到回收站失败:", dir, e)
					} else {
						os.WriteFile(filepath.Join(tar, DELETE_FLAG), []byte(time.Now().Format(time.RFC3339)), 0644)
					}
				}
			} else {
				log.Println("没有帖子需要删除")
			}
		}()

		c.JSON(http.StatusOK, id)
	}
}

func (srv *Server) topicUpdate() func(c *gin.Context) {
	return func(c *gin.Context) {
		id, e := strconv.Atoi(c.Param("id"))
		if e != nil {
			c.JSON(http.StatusBadRequest, toErr("无效的帖子 ID"))
			return
		}

		topic, has := srv.cache.topics.Get(id)
		if !has {
			c.JSON(http.StatusNotFound, toErr("未找到帖子"))
			return
		}

		md := new(Metadata)
		if e := c.ShouldBindJSON(md); e != nil {
			c.JSON(http.StatusBadRequest, toErr("无效的请求数据"))
			return
		}
		uc := md.UpdateCron
		if uc != "" {
			_, e := cron.ParseStandard(uc)
			if e != nil {
				c.JSON(http.StatusBadRequest, toErr("无效的 cron 表达式"))
				return
			}
		}
		topic.Metadata.Merge(md)
		srv.addCron(topic)
		topic.Stop()
		topic.Modify()
		go topic.Save()

		c.JSON(http.StatusOK, id)
	}
}

func (srv *Server) topicFresh() func(c *gin.Context) {
	return func(c *gin.Context) {
		id, e := strconv.Atoi(c.Param("id"))
		if e != nil {
			c.JSON(http.StatusBadRequest, toErr("无效的帖子 ID"))
			return
		}

		cache := srv.cache

		if !cache.topics.Has(id) {
			c.JSON(http.StatusNotFound, toErr("未找到帖子"))
			return
		}

		select {
		case cache.queue <- id:
			c.JSON(http.StatusOK, id)
		default:
			c.JSON(http.StatusServiceUnavailable, toErr("添加请求过多"))
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
			c.String(http.StatusBadRequest, "无效的文件名")
			return
		}

		if tid == "smile" { // 处理使用默认表情设置的情况
			srv.replaySmile(c, name)
			return
		}

		id, e := strconv.Atoi(tid)
		if e != nil {
			c.String(http.StatusBadRequest, "无效的帖子 ID")
			return
		}

		cache := srv.cache
		topic, has := cache.topics.Get(id)
		if !has {
			c.String(http.StatusNotFound, "未找到帖子")
			return
		}

		if strings.HasPrefix(name, "at_") { // load attachment failed from NGA
			log.Println(name, "是附件")
			srv.replayAttachment(c, name[3:], topic)
			return
		}

		// 确保文件路径在 root 目录下, 防止路径穿越
		file := filepath.Join(topic.root, filepath.Clean(name))
		if !strings.HasPrefix(file, topic.root) {
			c.String(http.StatusBadRequest, "非法文件名")
			return
		}

		data, e := os.ReadFile(file)
		if e != nil {
			c.String(http.StatusInternalServerError, "读取资源失败")
			return
		}
		c.Data(http.StatusOK, ContentType(name), data)
	}
}

func (srv *Server) replayAttachment(c *gin.Context, name string, topic *Topic) {
	i := strings.IndexByte(name, '_')
	if i < 0 {
		c.String(http.StatusBadRequest, "无效的附件名，缺少楼层")
		return
	}

	src, e := url.QueryUnescape(strings.ReplaceAll(name[i+1:], "_2F", "%2F"))
	if e != nil {
		c.String(http.StatusBadRequest, "无效的附件名，解码失败")
		return
	}
	log.Println("附件来源", src)
	ext := filepath.Ext(src)
	dir := filepath.Join(topic.root, ATTACH_DIR)
	os.MkdirAll(dir, os.ModePerm)
	fn := name[:i] + "_" + ShortSha1(src) + ext
	file := filepath.Join(dir, fn)
	if IsExist(file) {
		f, e := os.Open(file)
		if e != nil {
			c.String(http.StatusInternalServerError, "打开附件文件失败")
			return
		}
		defer f.Close()

		log.Println("命中附件缓存", fn)

		c.Header("Content-Type", ContentType(fn))
		if _, e := io.Copy(c.Writer, f); e != nil {
			c.String(http.StatusInternalServerError, "复制附件内容失败")
		}
		return
	}

	if strings.Contains(src, ".nga.178.com/attachments/") {
		log.Println("获取附件", src)
		req, e := http.NewRequest(http.MethodGet, src, nil)
		if e != nil {
			c.String(http.StatusInternalServerError, "无效的附件名，创建请求失败")
			return
		}
		req.Header.Set("User-Agent", srv.nga.GetUA())

		resp, e := DoHttp(req)
		if e != nil {
			c.String(http.StatusInternalServerError, "获取附件失败")
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			c.String(http.StatusInternalServerError, "获取附件失败，状态码: "+strconv.Itoa(resp.StatusCode))
			return
		}

		f, e := os.Create(file)
		if e != nil {
			c.String(http.StatusInternalServerError, "打开附件文件失败")
			return
		}
		defer f.Close()

		c.Header("Content-Type", resp.Header.Get("Content-Type"))
		c.Header("Content-Length", resp.Header.Get("Content-Length"))
		iw := io.MultiWriter(c.Writer, f)
		if _, e := io.Copy(iw, resp.Body); e != nil {
			c.String(http.StatusInternalServerError, "复制附件内容失败")
		}
	} else {
		c.String(http.StatusBadRequest, "无效的附件名，不是 NGA 的附件")
	}
}

func (srv *Server) replaySmile(c *gin.Context, name string) {
	cache := srv.cache
	if cache.smile == nil {
		cache.lock.Lock()
		defer cache.lock.Unlock()

		data, e := efs.ReadFile("assets/smiles.json")
		if e != nil {
			c.String(http.StatusInternalServerError, "加载内嵌的 smiles.json 失败")
			return
		}
		if smile, e := Unmarshal(data); e != nil {
			c.String(http.StatusInternalServerError, "解析内嵌的 smiles.json 失败")
			return
		} else {
			smile.root = filepath.Clean(filepath.Join(cache.topicRoot, SMILE_DIR))
			cache.smile = smile
		}
	}

	if srv.Cfg.Smile == "web" {
		url := cache.smile.URL(name)
		if url == "" {
			log.Printf("未找到表情 %s\n", name)
			c.String(http.StatusNotFound, "未找到表情 "+name)
		} else {
			c.Redirect(http.StatusMovedPermanently, url)
		}
	} else {
		data, e := cache.smile.Local(name, srv.nga.GetUA())
		if e != nil {
			c.String(http.StatusInternalServerError, "加载表情失败: "+e.Error())
		} else if data == nil {
			c.String(http.StatusNotFound, "未找到表情 "+name)
		} else {
			c.Data(http.StatusOK, ContentType(name), data)
		}
	}
}

type viewTplData struct {
	Title    string
	ID       int
	Token    string
	BaseUrl  string
	Markdown template.HTML
	Version  string
}

func (srv *Server) viewTopic() func(c *gin.Context) {
	return func(c *gin.Context) {
		title, markdown := "", ""

		id, e := strconv.Atoi(c.Param("id"))
		if e != nil {
			title = "无效的帖子 ID"
		} else {
			topic, has := srv.cache.topics.Get(id)
			if !has {
				title = "未找到帖子"
			} else {
				title = topic.Title
				markdown, e = topic.Content()
				if e != nil {
					title = "读取帖子失败"
				} else {
					markdown += "----\n"
				}
			}
		}

		tmpl, e := template.ParseFS(efs, "assets/view.html")
		if e != nil {
			c.String(http.StatusInternalServerError, "加载查看页面失败")
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
			Version:  srv.Cfg.GitHash,
		}
		c.Header("Content-Type", HTML_HEADER)
		if e := tmpl.Execute(c.Writer, data); e != nil {
			c.String(http.StatusInternalServerError, "渲染查看页面失败")
		}
	}
}

func (srv *Server) homePage() func(c *gin.Context) {
	return func(c *gin.Context) {
		tmpl, e := template.ParseFS(efs, "assets/home.html")
		if e != nil {
			c.String(http.StatusInternalServerError, "加载主页失败")
			return
		}
		data := struct {
			HasToken        bool
			BaseUrl         string
			Version         string
			DefaultMaxRetry int
		}{
			HasToken:        srv.Cfg.Token != "",
			BaseUrl:         srv.nga.BaseURL(),
			Version:         srv.Cfg.GitHash,
			DefaultMaxRetry: DEFAULT_MAX_RETRY,
		}
		c.Header("Content-Type", HTML_HEADER)
		if e := tmpl.Execute(c.Writer, data); e != nil {
			c.String(http.StatusInternalServerError, "渲染查看页面失败")
		}
	}
}

func (srv *Server) favicon() func(c *gin.Context) {
	return func(c *gin.Context) {
		data, err := efs.ReadFile("assets/favicon.ico")
		if err != nil {
			c.String(http.StatusInternalServerError, "读取 favicon.ico 失败")
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
			c.String(http.StatusInternalServerError, "读取资源失败 "+name)
			return
		}
		c.Data(http.StatusOK, ContentType(name), data)
	}
}

func (srv *Server) subscribeStatus() func(c *gin.Context) {
	return func(c *gin.Context) {
		name := c.Param("name")
		if user, e := srv.nga.GetUser(name); e == nil {
			c.JSON(http.StatusOK, user.Subscribed)
		} else {
			c.JSON(http.StatusOK, false)
		}
	}
}
func (srv *Server) subscribeBatchStatus() func(c *gin.Context) {
	return func(c *gin.Context) {
		var names []string
		if e := c.ShouldBindJSON(&names); e != nil {
			c.JSON(http.StatusBadRequest, toErr("无效的请求数据"))
			return
		}
		users := make(map[string]User)
		for _, name := range names {
			if user, e := srv.nga.GetUser(name); e == nil {
				users[name] = user
			}
		}
		c.JSON(http.StatusOK, users)
	}
}
func (srv *Server) subscribe() func(c *gin.Context) {
	return func(c *gin.Context) {
		name := c.Param("name")
		cond := make([]string, 0)
		if e := c.ShouldBindJSON(&cond); e != nil {
			c.JSON(http.StatusBadRequest, toErr("无效的请求数据"))
			return
		}
		if len(cond) > 0 {
			for i, c := range cond {
				cond[i] = strings.TrimSpace(c)
			}
		}
		if user, e := srv.nga.GetUser(name); e == nil {
			if e = srv.nga.Subscribe(user.Id, true, cond...); e == nil {
				user, _ = srv.nga.GetUser(name)
				c.JSON(http.StatusOK, user)
			} else {
				c.JSON(http.StatusInternalServerError, toErr(e.Error()))
			}
		} else {
			c.JSON(http.StatusInternalServerError, toErr(e.Error()))
		}
	}
}
func (srv *Server) unsubscribe() func(c *gin.Context) {
	return func(c *gin.Context) {
		name := c.Param("name")
		if user, e := srv.nga.GetUser(name); e == nil {
			if e = srv.nga.Subscribe(user.Id, false); e == nil {
				c.JSON(http.StatusOK, user.Id)
			} else {
				c.JSON(http.StatusInternalServerError, toErr(e.Error()))
			}
		} else {
			c.JSON(http.StatusInternalServerError, toErr(e.Error()))
		}
	}
}
