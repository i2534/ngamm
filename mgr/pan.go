package mgr

import (
	"encoding/json"
	"html"
	"io"
	"log"
	"regexp"
	"strings"
)

const PAN_JSON = "pan.json"

var (
	panURLRegex *regexp.Regexp = regexp.MustCompile(`\((https://pan\..+)\)`)
	panTqmRegex *regexp.Regexp = regexp.MustCompile(`提取码[：:\s]*([a-zA-Z0-9]{4})`)
	panPwdRegex *regexp.Regexp = regexp.MustCompile(`解压密码[：:\s统一为]*(.+)`)
)

// 网盘
type Pan interface {
	io.Closer
	Name() string
	Init() error
	Support(pmd PanMetadata) bool
	// 保存分享到网盘, 实现自行处理队列
	Transfer(topicId int, pmd PanMetadata) error
}

type PanMetadata struct {
	URL   string // 网盘链接
	Tqm   string // 提取码
	Pwd   string // 解压密码
	Saved bool   // 是否已保存
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
