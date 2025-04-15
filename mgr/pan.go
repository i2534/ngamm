package mgr

import (
	"encoding/json"
	"html"
	"io"
	"log"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/ini.v1"
)

var (
	PAN_CONFIG = "config.ini"
	PAN_JSON   = "pan.json"

	panURLRegex *regexp.Regexp = regexp.MustCompile(`\((https://pan\..+)\)`)
	panTqmRegex *regexp.Regexp = regexp.MustCompile(`提取码[：:\s]*([a-zA-Z0-9]{4})`)
	panPwdRegex *regexp.Regexp = regexp.MustCompile(`解压密码[：:\s统一为]*(.+)`)
)

// 网盘
type Pan interface {
	io.Closer
	Name() string
	Init() error
	Support(md PanMetadata) bool
	// 保存分享到网盘, 实现自行处理队列
	Transfer(topicId int, md PanMetadata) error
}

type PanMetadata struct {
	URL   string // 网盘链接
	Tqm   string // 提取码
	Pwd   string // 解压密码
	Saved bool   // 是否已保存
}

func InitPan(root string) ([]Pan, error) {
	fp := filepath.Join(root, PAN_CONFIG)
	cfg, e := ini.Load(fp)
	if e != nil {
		log.Printf("加载网盘配置文件 %s 失败: %s\n", fp, e.Error())
		return nil, e
	}

	ps := make([]Pan, 0)

	cbs := cfg.Section("baidu")
	if cbs != nil {
		if b, _ := cbs.Key("enable").Bool(); b {
			bc := BaiduCfg{
				Root:   filepath.Join(root, "baidu"),
				Bduss:  cbs.Key("bduss").String(),
				Stoken: cbs.Key("stoken").String(),
			}
			baidu := NewBaidu(bc)
			if e := baidu.Init(); e != nil {
				log.Println("初始化失败:", e.Error())
			} else {
				log.Println("BaiduPan 初始化完成")
				ps = append(ps, baidu)
			}
		} else {
			log.Println("BaiduPan 未启用")
		}
	}
	cqs := cfg.Section("quark")
	if cqs != nil {
		if b, _ := cqs.Key("enable").Bool(); b {
			qc := QuarkCfg{
				Root:   filepath.Join(root, "quark"),
				Cookie: cqs.Key("cookie").String(),
			}
			quark := NewQuarkPan(qc)
			if e := quark.Init(); e != nil {
				log.Println("初始化失败:", e.Error())
			} else {
				log.Println("QuarkPan 初始化完成")
				ps = append(ps, quark)
			}
		} else {
			log.Println("QuarkPan 未启用")
		}
	}
	return ps, nil
}

func (t *Topic) TryTransfer(pans *SyncMap[string, Pan]) {
	if pans == nil || pans.Size() == 0 {
		return
	}

	t.Metadata.mutex.Lock()
	defer t.Metadata.mutex.Unlock()

	if t.root.IsExist(PAN_JSON) {
		return
	}

	pms, e := t.GetPanMetadata()
	if e != nil {
		log.Printf("获取网盘链接信息失败: %s\n", e.Error())
		return
	}
	for _, pm := range pms {
		for _, pan := range pans.Values() {
			if !pan.Support(*pm) {
				continue
			}
			if e := pan.Transfer(t.Id, *pm); e != nil {
				log.Printf("保存 %d 到网盘 %s 失败: %s\n", t.Id, pan.Name(), e.Error())
				continue
			}
			pm.Saved = true
		}
	}

	data, e := json.MarshalIndent(pms, "", "  ")
	if e != nil {
		log.Printf("持久化网盘 JSON 失败: %s\n", e.Error())
		return
	}
	if e := t.root.WriteAll(PAN_JSON, data); e != nil {
		log.Printf("保存网盘 JSON 失败: %s\n", e.Error())
		return
	}
}

func (t *Topic) GetPanMetadata() ([]*PanMetadata, error) {
	pms := make([]*PanMetadata, 0)
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
			pms = append(pms, pm)
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

	for _, pm := range pms {
		pm.Pwd = pwd
	}
	return pms, e
}
