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
	"strings"

	"gopkg.in/ini.v1"
)

const (
	TRANSFER_AUTO   = "auto"   // 自动转存
	TRANSFER_MANUAL = "manual" // 手动转存
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
	Support(md PanMetadata, transferType string) bool
	// 保存分享到网盘, 实现自行处理队列
	Transfer(topicId int, md PanMetadata) error
	SetHolder(holder *PanHolder)
}

type PanMetadata struct {
	URL   string // 网盘链接
	Tqm   string // 提取码
	Pwd   string // 解压密码
	Saved bool   // 是否已保存
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

type PanHolder struct {
	Root  string      // 网盘根目录
	Pans  []Pan       // 网盘列表
	hooks []webhook   // 网盘钩子
	msgCh chan string // 网盘消息
}

func (p *PanHolder) Close() error {
	for _, pan := range p.Pans {
		pan.Close()
	}
	close(p.msgCh)
	return nil
}

func NewPanHolder(root string) (*PanHolder, error) {
	fp := filepath.Join(root, PAN_CONFIG)
	cfg, e := ini.Load(fp)
	if e != nil {
		log.Printf("加载网盘配置文件 %s 失败: %s\n", fp, e.Error())
		return nil, e
	}
	cfg.BlockMode = false

	ph := &PanHolder{
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

func (p *PanHolder) Send(msg string) {
	if p.msgCh != nil {
		p.msgCh <- msg
	}
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
				bc.Transfer = TRANSFER_AUTO
			}
			bc.Root = filepath.Join(p.Root, "baidu")
			baidu := NewBaidu(*bc)
			if e := baidu.Init(); e != nil {
				log.Println("初始化失败:", e.Error())
				p.Send(e.Error())
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
				qc.Transfer = TRANSFER_AUTO
			}
			qc.Root = filepath.Join(p.Root, "quark")
			quark := NewQuarkPan(*qc)
			if e := quark.Init(); e != nil {
				log.Println("初始化失败:", e.Error())
				p.Send(e.Error())
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

	mds, e := t.GetPanMetadata()
	if e != nil {
		log.Printf("获取网盘链接信息失败: %s\n", e.Error())
		return
	}
	for _, md := range mds {
		for _, pan := range ph.Pans {
			if !pan.Support(*md, TRANSFER_AUTO) {
				continue
			}
			if e := pan.Transfer(t.Id, *md); e != nil {
				log.Printf("保存 %d 到网盘 %s 失败: %s\n", t.Id, pan.Name(), e.Error())
				continue
			}
			md.Saved = true
		}
	}

	data, e := json.MarshalIndent(mds, "", "  ")
	if e != nil {
		log.Printf("持久化网盘 JSON 失败: %s\n", e.Error())
	} else if e := t.root.WriteAll(PAN_JSON, data); e != nil {
		log.Printf("保存网盘 JSON 失败: %s\n", e.Error())
	}
}

func (t *Topic) GetPanMetadata() ([]*PanMetadata, error) {
	mds := make([]*PanMetadata, 0)
	floor := 0

	pwd := ""
	var pm *PanMetadata
	e := t.root.EveryLine(POST_MARKDOWN, func(line string, _ int) bool {
		nl := strings.TrimSpace(line)

		m := panURLRegex.FindStringSubmatch(nl)
		if len(m) > 1 {
			url := strings.TrimSpace(m[1])
			pm = &PanMetadata{
				URL: url,
			}
			mds = append(mds, pm)
		}
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

	for _, md := range mds {
		md.Pwd = pwd
	}
	return mds, e
}
