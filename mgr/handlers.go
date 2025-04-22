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
	has := srv.Cfg.Config.Token != ""

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
		vg.DELETE("/:token/:id", srv.topicForceReload())
	}

	pg := r.Group("/pan")
	{
		if has {
			pg.Use(srv.viewMiddleware())
		}
		pg.GET("/:token/:id", srv.topicPanRecords())
		pg.POST("/:token/:id", srv.topicPanOperate())
	}

	r.GET("/", srv.homePage())
	r.GET("/favicon.ico", srv.favicon())
	r.GET("/asset/:name", srv.asset())

	r.GET("/to", srv.test(http.StatusOK))
	r.GET("/ts", srv.test(http.StatusServiceUnavailable))
}

func toErr(msg string) gin.H {
	return gin.H{ERR_KEY: msg}
}

func (srv *Server) topicMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := c.GetHeader("Authorization")
		if token != srv.Cfg.Config.Token {
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

	r, e := cache.topicRoot.SafeOpenRoot(strconv.Itoa(id))
	if e != nil {
		return e
	}

	log.Printf("从 %s 加载帖子\n", r.Name())

	topic := NewTopic(r, id)
	topic.Create = Now()
	topic.Metadata.UpdateCron = DEFAULT_CRON

	cache.topics.Put(id, topic)

	select {
	case cache.queue <- id:
		srv.addCron(topic)
		go topic.SaveMeta()

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

		if !srv.deleteTopic(id) {
			c.JSON(http.StatusNotFound, toErr("未找到帖子"))
		} else {
			c.JSON(http.StatusOK, id)
		}
	}
}

func (srv *Server) deleteTopic(id int) bool {
	cache := srv.cache

	topic, has := cache.topics.Get(id)
	if !has {
		return false
	}

	cache.topics.Delete(id)

	go func() {
		log.Println("删除帖子", id)
		defer topic.Close()

		dir, e := topic.root.AbsPath()
		if e != nil {
			log.Println("获取帖子绝对路径失败:", e)
			return
		}

		root, e := cache.topicRoot.AbsPath()
		if e != nil {
			log.Println("获取帖子根目录绝对路径失败:", e)
			return
		}
		recycles := filepath.Join(root, DIR_RECYCLE_BIN)
		if e := os.MkdirAll(recycles, COMMON_DIR_MODE); e != nil {
			log.Println("创建回收站失败:", recycles, e)

			log.Println("尝试直接删除帖子:", dir)
			if e := os.RemoveAll(dir); e != nil {
				log.Println("直接删除帖子失败:", dir, e)
			}
		} else {
			log.Println("移动帖子到回收站:", dir)
			tar := filepath.Join(recycles, strconv.Itoa(id))
			os.RemoveAll(tar) // remove old
			if e := os.Rename(dir, tar); e != nil {
				log.Println("移动帖子到回收站失败:", dir, e)
			} else {
				os.WriteFile(filepath.Join(tar, DELETE_FLAG), []byte(time.Now().Format(time.RFC3339)), COMMON_FILE_MODE)
			}
		}
	}()

	return true
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

		md := NewMetadata()
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
		go topic.SaveMeta()

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
			c.JSON(http.StatusBadRequest, "无效的文件名")
			return
		}

		if tid == "smile" { // 处理使用默认表情设置的情况
			srv.replaySmile(c, name)
			return
		}

		id, e := strconv.Atoi(tid)
		if e != nil {
			c.JSON(http.StatusBadRequest, "无效的帖子 ID")
			return
		}

		cache := srv.cache
		topic, has := cache.topics.Get(id)
		if !has {
			c.JSON(http.StatusNotFound, "未找到帖子")
			return
		}

		if strings.HasPrefix(name, "at_") { // load attachment failed from NGA
			log.Println(name, "是附件")
			srv.replayAttachment(c, name[3:], topic)
			return
		}

		f, e := topic.root.Open(name)
		if e != nil {
			c.JSON(http.StatusInternalServerError, "读取资源失败")
			return
		}
		defer f.Close()

		c.DataFromReader(http.StatusOK, FileSize(f), ContentType(name), f, nil)
	}
}

func (srv *Server) replayAttachment(c *gin.Context, name string, topic *Topic) {
	i := strings.IndexByte(name, '_')
	if i < 0 {
		c.JSON(http.StatusBadRequest, "无效的附件名，缺少楼层")
		return
	}

	src, e := url.QueryUnescape(strings.ReplaceAll(name[i+1:], "_2F", "%2F"))
	if e != nil {
		c.JSON(http.StatusBadRequest, "无效的附件名，解码失败")
		return
	}
	log.Println("附件来源", src)
	fn := name[:i] + "_" + ShortSha1(src) + filepath.Ext(src)
	fp := filepath.Join(ATTACH_DIR, fn)
	if topic.root.IsExist(fp) {
		f, e := topic.root.OpenReader(fp)
		if e != nil {
			c.JSON(http.StatusInternalServerError, "打开附件文件失败")
			return
		}
		defer f.Close()

		log.Println("命中附件缓存", fn)

		c.DataFromReader(http.StatusOK, FileSize(f), ContentType(fn), f, nil)
		return
	}

	if strings.Contains(src, ".nga.178.com/attachments/") {
		log.Println("获取附件", src)
		req, e := http.NewRequest(http.MethodGet, src, nil)
		if e != nil {
			c.JSON(http.StatusInternalServerError, "无效的附件名，创建请求失败")
			return
		}
		req.Header.Set("User-Agent", srv.nga.GetUA())

		resp, e := DoHttp(req)
		if e != nil {
			c.JSON(http.StatusInternalServerError, "获取附件失败")
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			c.JSON(http.StatusInternalServerError, "获取附件失败，状态码: "+strconv.Itoa(resp.StatusCode))
			return
		}

		f, e := topic.root.OpenWriter(fp)
		if e != nil {
			c.JSON(http.StatusInternalServerError, "打开附件文件失败")
			return
		}
		defer f.Close()

		c.Header("Content-Type", resp.Header.Get("Content-Type"))
		c.Header("Content-Length", resp.Header.Get("Content-Length"))
		iw := io.MultiWriter(c.Writer, f)
		if _, e := io.Copy(iw, resp.Body); e != nil {
			c.JSON(http.StatusInternalServerError, "复制附件内容失败")
		}
	} else {
		c.JSON(http.StatusBadRequest, "无效的附件名，不是 NGA 的附件")
	}
}

func (srv *Server) replaySmile(c *gin.Context, name string) {
	cache := srv.cache
	if cache.smile == nil {
		cache.lock.Lock()
		defer cache.lock.Unlock()

		data, e := efs.ReadFile("assets/smiles.json")
		if e != nil {
			c.JSON(http.StatusInternalServerError, "加载内嵌的 smiles.json 失败")
			return
		}
		if smile, e := Unmarshal(data); e != nil {
			c.JSON(http.StatusInternalServerError, "解析内嵌的 smiles.json 失败")
			return
		} else {
			dir, e := cache.topicRoot.SafeOpenRoot(SMILE_DIR)
			if e != nil {
				c.JSON(http.StatusInternalServerError, "获取表情目录失败")
				return
			}
			smile.root = dir
			cache.smile = smile
		}
	}

	if srv.Cfg.Config.Smile == "web" {
		url := cache.smile.URL(name)
		if url == "" {
			log.Printf("未找到表情 %s\n", name)
			c.JSON(http.StatusNotFound, "未找到表情 "+name)
		} else {
			c.Redirect(http.StatusMovedPermanently, url)
		}
	} else {
		data, e := cache.smile.Local(name, srv.nga.GetUA())
		if e != nil {
			c.JSON(http.StatusInternalServerError, "加载表情失败: "+e.Error())
		} else if data == nil {
			c.JSON(http.StatusNotFound, "未找到表情 "+name)
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
			HasToken:        srv.Cfg.Config.Token != "",
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
			c.JSON(http.StatusInternalServerError, "读取 favicon.ico 失败")
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
			c.JSON(http.StatusInternalServerError, "读取资源失败 "+name)
			return
		}
		c.Data(http.StatusOK, ContentType(name), data)
	}
}

func (srv *Server) subscribeStatus() func(c *gin.Context) {
	return func(c *gin.Context) {
		name := c.Param("name")
		uid, e := strconv.Atoi(name)
		if e == nil {
			if user, e := srv.nga.GetUserById(uid); e == nil {
				c.JSON(http.StatusOK, user.Subscribed)
				return
			}
		} else {
			log.Println("解析用户 ID 失败:", name, e)
		}
		c.JSON(http.StatusOK, false)
	}
}
func (srv *Server) subscribeBatchStatus() func(c *gin.Context) {
	return func(c *gin.Context) {
		var uids []int
		if e := c.ShouldBindJSON(&uids); e != nil {
			c.JSON(http.StatusBadRequest, toErr("无效的请求数据"))
			return
		}
		users := make(map[int]User)
		for _, uid := range uids {
			if user, e := srv.nga.GetUserById(uid); e == nil {
				users[uid] = user
			}
		}
		c.JSON(http.StatusOK, users)
	}
}
func (srv *Server) subscribe() func(c *gin.Context) {
	return func(c *gin.Context) {
		uid, e := strconv.Atoi(c.Param("name"))
		if e != nil {
			c.JSON(http.StatusBadRequest, toErr("无效的用户 ID"))
			return
		}

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
		if user, e := srv.nga.GetUserById(uid); e == nil {
			if e = srv.nga.Subscribe(user.Id, true, cond...); e == nil {
				user, _ = srv.nga.GetUserById(uid)
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
		uid, e := strconv.Atoi(c.Param("name"))
		if e != nil {
			c.JSON(http.StatusBadRequest, toErr("无效的用户 ID"))
			return
		}
		if user, e := srv.nga.GetUserById(uid); e == nil {
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

func (srv *Server) topicForceReload() func(c *gin.Context) {
	return func(c *gin.Context) {
		id, e := strconv.Atoi(c.Param("id"))
		if e != nil {
			c.JSON(http.StatusBadRequest, toErr("无效的帖子 ID"))
			return
		}

		if !srv.deleteTopic(id) {
			c.JSON(http.StatusInternalServerError, toErr("删除帖子失败"))
			return
		}

		if e := srv.addTopic(id); e != nil {
			c.JSON(http.StatusInternalServerError, toErr(e.Error()))
			return
		}

		c.JSON(http.StatusOK, id)
	}
}

func (srv *Server) test(code int) func(c *gin.Context) {
	return func(c *gin.Context) {
		c.String(code, "Test测试")
	}
}
