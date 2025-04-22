package mgr

import (
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"gopkg.in/ini.v1"
)

type PanOpt uint

const (
	TRANSFER_TYPE_AUTO   = "auto"   // 自动转存
	TRANSFER_TYPE_MANUAL = "manual" // 手动转存

	TRANSFER_STATUS_PENDING = "pending" // 待处理
	TRANSFER_STATUS_SUCCESS = "success" // 成功
	TRANSFER_STATUS_FAILED  = "failed"  // 失败

	PAN_OPT_SAVE   PanOpt = 1 // 保存
	PAN_OPT_DELETE PanOpt = 2 // 删除
)

var (
	PAN_CONFIG   = "config.ini"
	PAN_JSON     = "pan.json"
	PAN_PWD_FILE = "_uzp.txt"

	panURLRegex *regexp.Regexp = regexp.MustCompile(`\((https://pan\..+)\)`)
	panTqmRegex *regexp.Regexp = regexp.MustCompile(`提取码[：:\s]*([a-zA-Z0-9]{4})`)
	panPwdRegex *regexp.Regexp = regexp.MustCompile(`解压密?码[：:\s统一为]*(.+)`)
)

// 网盘
type Pan interface {
	io.Closer
	Name() string
	Init() error
	Support(record TransferRecord) bool
	TransferType() string
	// 保存分享到网盘, 实现自行处理队列
	Transfer(topicId int, record TransferRecord) error
	SetHolder(holder *PanHolder)
	Operate(topicId int, record *TransferRecord, opt PanOpt) error
}

type TransferRecord struct {
	Name    string // 网盘名称
	URL     string // 网盘链接
	Tqm     string // 提取码
	Pwd     string // 解压密码
	Saved   *bool  `json:"Saved,omitempty"` // 是否已保存, 旧属性, 由 Status 代替
	Status  string // 状态
	Message string // 错误信息
}

func (r *TransferRecord) ChangeStatus(status string, message string) {
	r.Status = status
	r.Message = message
	r.Saved = nil
}

type webhook struct {
	Enable bool   `ini:"enable"` // 是否启用
	Name   string `ini:"name"`   // 名称
	URL    string `ini:"url"`    // 网钩地址
	Method string `ini:"method"` // 请求方法
	Header string `ini:"header"` // 请求头
	Body   string `ini:"body"`   // 请求体
}

func (w webhook) send(msg string) error {
	data := strings.ReplaceAll(w.Body, "{{message}}", msg)
	log.Println("webhook:", w.Name, "发送:", data)
	req, e := http.NewRequest(w.Method, w.URL, strings.NewReader(data))
	if e != nil {
		return e
	}
	if w.Header != "" {
		for _, h := range strings.Split(w.Header, ";") {
			kv := strings.SplitN(h, ":", 2)
			if len(kv) != 2 {
				continue
			}
			k := strings.TrimSpace(kv[0])
			v := strings.TrimSpace(kv[1])
			req.Header.Set(k, v)
		}
	}
	resp, e := DoHttp(req)
	if e != nil {
		return e
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("webhook %s 返回错误: %d, %s", w.Name, resp.StatusCode, string(body))
	}
	log.Println("webhook:", w.Name, "返回:", string(body))
	return nil
}

type topicCache struct {
	topic   *Topic
	records []*TransferRecord
}

var (
	panTopicCache *LRUMap[int, *topicCache] = NewLRUMap[int, *topicCache]().WithCapacity(10)
)

func cachePanTopic(topic *Topic, records []*TransferRecord) {
	if topic == nil || records == nil {
		return
	}
	panTopicCache.Put(topic.Id, &topicCache{
		topic,
		records,
	})
}

type PanHolder struct {
	Root  string      // 网盘根目录
	Pans  []Pan       // 网盘列表
	hooks []webhook   // 网盘钩子
	msgCh chan string // 网盘消息
	srv   *Server
}

func (p *PanHolder) Close() error {
	for _, pan := range p.Pans {
		pan.Close()
	}
	close(p.msgCh)
	return nil
}

func NewPanHolder(root string, srv *Server) (*PanHolder, error) {
	fp := filepath.Join(root, PAN_CONFIG)
	cfg, e := ini.Load(fp)
	if e != nil {
		log.Printf("加载网盘配置文件 %s 失败: %s\n", fp, e.Error())
		return nil, e
	}
	cfg.BlockMode = false

	ph := &PanHolder{
		srv:   srv,
		Root:  root,
		Pans:  make([]Pan, 0),
		hooks: make([]webhook, 0),
		msgCh: make(chan string, 99),
	}

	ph.initHook(cfg)
	ph.initBaidu(cfg.Section("baidu"))
	ph.initQuark(cfg.Section("quark"))

	go func() {
		for msg := range ph.msgCh {
			log.Println("准备发送网盘消息:", msg)
			for _, hook := range ph.hooks {
				if e := hook.send(msg); e != nil {
					log.Printf("发送网盘消息到 %s 失败: %s\n", hook.Name, e.Error())
				}
			}
		}
	}()

	return ph, nil
}

func (p *PanHolder) initBaidu(cfg *ini.Section) {
	if cfg == nil {
		return
	}

	bc := new(BaiduCfg)
	if e := cfg.MapTo(bc); e != nil {
		log.Println("BaiduPan 配置解析失败:", e.Error())
	} else {
		if bc.Enable {
			if bc.Transfer == "" {
				bc.Transfer = TRANSFER_TYPE_AUTO
			}
			bc.Root = filepath.Join(p.Root, "baidu")
			baidu := NewBaidu(*bc)
			if e := baidu.Init(); e != nil {
				log.Println("初始化失败:", e.Error())
				p.msgCh <- e.Error()
			} else {
				log.Println("BaiduPan 初始化完成")
				baidu.SetHolder(p)
				p.Pans = append(p.Pans, baidu)
			}
		} else {
			log.Println("BaiduPan 未启用")
		}
	}
}
func (p *PanHolder) initQuark(cfg *ini.Section) {
	if cfg == nil {
		return
	}
	qc := new(QuarkCfg)
	if e := cfg.MapTo(qc); e != nil {
		log.Println("QuarkPan 配置解析失败:", e.Error())
	} else {
		if qc.Enable {
			if qc.Transfer == "" {
				qc.Transfer = TRANSFER_TYPE_AUTO
			}
			qc.Root = filepath.Join(p.Root, "quark")
			quark := NewQuarkPan(*qc)
			if e := quark.Init(); e != nil {
				log.Println("初始化失败:", e.Error())
				p.msgCh <- e.Error()
			} else {
				log.Println("QuarkPan 初始化完成")
				quark.SetHolder(p)
				p.Pans = append(p.Pans, quark)
			}
		} else {
			log.Println("QuarkPan 未启用")
		}
	}
}

func (p *PanHolder) initHook(cfg *ini.File) {
	if cfg == nil {
		return
	}
	for _, sec := range cfg.Sections() {
		name := sec.Name()
		if strings.Contains(name, "webhook.") {
			log.Println("初始化 webhook:", name)
			hook := new(webhook)
			if e := sec.MapTo(hook); e != nil {
				log.Println("初始化失败:", e.Error())
				continue
			}
			if hook.Enable {
				log.Println("启用 webhook:", hook.Name)
				p.hooks = append(p.hooks, *hook)
			} else {
				log.Println("未启用 webhook:", hook.Name)
			}
		}
	}
}

func (p *PanHolder) notify(topicId int, url, status, msg string) {
	if status == TRANSFER_STATUS_FAILED && msg != "" {
		p.msgCh <- msg
	}

	if topic, has := p.srv.cache.topics.Get(topicId); has {
		topic.Metadata.mutex.Lock()
		defer topic.Metadata.mutex.Unlock()

		if records, e := topic.loadPanRecords(); e != nil {
			log.Println("加载网盘记录失败:", e.Error())
		} else {
			changed := false
			for _, r := range records {
				if r.URL == url {
					r.ChangeStatus(status, msg)
					changed = true
					break
				}
			}
			if changed {
				if e := topic.savePanRecords(records); e != nil {
					log.Println("保存网盘记录失败:", e.Error())
				}
			}
		}
	} else {
		log.Println("未找到帖子:", topicId)
	}
}

// 自动转存
func (t *Topic) AutoTransfer(ph *PanHolder) {
	if ph == nil {
		return
	}
	t.Metadata.mutex.Lock()
	defer t.Metadata.mutex.Unlock()

	if t.root.IsExist(PAN_JSON) {
		return
	}

	rs, e := t.ParseTransferRecord()
	if e != nil {
		log.Printf("获取网盘链接信息失败: %s\n", e.Error())
		return
	}

	cachePanTopic(t, rs)

	changed := false
	for _, r := range rs {
		for _, pan := range ph.Pans {
			if pan.TransferType() != TRANSFER_TYPE_AUTO || !pan.Support(*r) {
				continue
			}
			r.Name = pan.Name()
			r.Status = TRANSFER_STATUS_PENDING
			if e := pan.Transfer(t.Id, *r); e != nil {
				r.ChangeStatus(TRANSFER_STATUS_FAILED, e.Error())
				log.Printf("保存 %d 到网盘 %s 失败: %s\n", t.Id, pan.Name(), e.Error())
			}
			changed = true
		}
	}

	if changed {
		if e := t.savePanRecords(rs); e != nil {
			log.Printf("保存网盘记录失败: %s\n", e.Error())
		}
	}
}

func (t *Topic) ParseTransferRecord() ([]*TransferRecord, error) {
	records := make([]*TransferRecord, 0)

	floor := 0
	pwd := ""
	var pm *TransferRecord
	e := t.root.EveryLine(POST_MARKDOWN, func(line string, _ int) bool {
		nl := strings.TrimSpace(line)

		m := panURLRegex.FindStringSubmatch(nl)
		if len(m) > 1 {
			url := strings.TrimSpace(m[1])

			// 去除重复的, 因为可能有引用
			for _, rec := range records {
				if rec.URL == url {
					return true
				}
			}

			pm = &TransferRecord{
				URL: url,
			}
			records = append(records, pm)

			m = panTqmRegex.FindStringSubmatch(nl)
			if len(m) > 1 {
				tqm := strings.TrimSpace(m[1])
				if pm != nil {
					pm.Tqm = tqm
				}
			}

			m = panPwdRegex.FindStringSubmatch(nl)
			if len(m) > 1 {
				pwd = strings.TrimSpace(m[1])
			}
		}
		if nl == "----" {
			floor++
		}
		if floor > 3 {
			return false
		}
		return true
	})

	if pwd != "" {
		pwd = html.UnescapeString(pwd)
	}

	for _, rec := range records {
		rec.Pwd = pwd
	}
	return records, e
}

func (t *Topic) loadPanRecords() ([]*TransferRecord, error) {
	if cache, has := panTopicCache.Get(t.Id); has {
		return cache.records, nil
	}

	if !t.root.IsExist(PAN_JSON) {
		return nil, fmt.Errorf("未找到网盘记录")
	}

	c, e := t.root.ReadAll(PAN_JSON)
	if e != nil {
		return nil, fmt.Errorf("读取网盘 JSON 失败: %s", e.Error())
	}

	var records []*TransferRecord
	if e := json.Unmarshal(c, &records); e != nil {
		return nil, fmt.Errorf("解析网盘 JSON 失败: %s", e.Error())
	}

	cachePanTopic(t, records)

	return records, nil
}

func (t *Topic) savePanRecords(records []*TransferRecord) error {
	data, e := json.MarshalIndent(records, "", "  ")
	if e != nil {
		return fmt.Errorf("持久化网盘 JSON 失败: %s", e.Error())
	} else if e := t.root.WriteAll(PAN_JSON, data); e != nil {
		return fmt.Errorf("保存网盘 JSON 失败: %s", e.Error())
	}
	return nil
}

func (srv *Server) topicPanRecords() func(c *gin.Context) {
	return func(c *gin.Context) {
		id, e := strconv.Atoi(c.Param("id"))

		if e != nil {
			c.JSON(http.StatusBadRequest, "无效的帖子 ID")
			return
		}

		if cache, has := panTopicCache.Get(id); has {
			c.JSON(http.StatusOK, cache.records)
			return
		}

		topic, has := srv.cache.topics.Get(id)
		if !has {
			c.JSON(http.StatusNotFound, "未找到帖子")
			return
		}

		topic.Metadata.mutex.Lock()
		defer topic.Metadata.mutex.Unlock()

		if records, e := topic.loadPanRecords(); e != nil {
			c.JSON(http.StatusInternalServerError, e.Error())
		} else {
			// 处理下旧数据
			changed := false
			for _, r := range records {
				if r.Name == "" {
					for _, pan := range srv.cache.pans.Pans {
						if pan.Support(*r) {
							r.Name = pan.Name()
							changed = true
							break
						}
					}
				}
				if r.Status == "" && r.Saved != nil {
					if *r.Saved {
						r.ChangeStatus(TRANSFER_STATUS_SUCCESS, "")
					} else {
						r.ChangeStatus(TRANSFER_STATUS_PENDING, "")
					}
					changed = true
				}
			}
			if changed {
				if e := topic.savePanRecords(records); e != nil {
					log.Println(e.Error())
				}
			}
			c.JSON(http.StatusOK, records)
		}
	}
}

func (srv *Server) topicPanOperate() func(c *gin.Context) {
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

		// 定义接收请求数据的结构体
		var form struct {
			Opt string `json:"opt"` // 操作类型， "save", "delete", "retry"
			URL string `json:"url"` // url
		}

		// 获取 POST 数据
		if e := c.ShouldBindJSON(&form); e != nil {
			log.Printf("解析请求数据失败: %s\n", e.Error())
			c.JSON(http.StatusBadRequest, toErr("无效的请求数据"))
			return
		}

		topic.Metadata.mutex.Lock()
		records, e := topic.loadPanRecords()
		if e != nil {
			c.JSON(http.StatusNotFound, toErr(e.Error()))
			topic.Metadata.mutex.Unlock()
			return
		}
		topic.Metadata.mutex.Unlock()

		var record *TransferRecord
		for _, r := range records {
			if r.URL == form.URL {
				record = r
				break
			}
		}
		if record == nil {
			c.JSON(http.StatusNotFound, toErr("未找到网盘记录"))
			return
		}

		var opt PanOpt
		switch form.Opt {
		case "save":
			log.Println("保存网盘记录:", record.URL)
			opt = PAN_OPT_SAVE
		case "delete":
			log.Println("删除网盘记录:", record.URL)
			opt = PAN_OPT_DELETE
		case "retry":
			log.Println("重试网盘记录:", record.URL)
			opt = PAN_OPT_SAVE
		}
		if opt == 0 {
			c.JSON(http.StatusBadRequest, toErr("无效的操作类型"))
			return
		}

		for _, pan := range srv.cache.pans.Pans {
			if pan.Support(*record) {
				if e := pan.Operate(topic.Id, record, opt); e != nil {
					c.JSON(http.StatusInternalServerError, toErr(e.Error()))
					return
				}
				break
			}
		}

		c.JSON(http.StatusOK, true)
	}
}
