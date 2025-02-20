package mgr

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
	"gopkg.in/ini.v1"
)

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

type DownResult struct {
	Success bool
	Message string
	Time    CustomTime
}

type Topic struct {
	root     string
	modAt    CustomTime
	timers   *SyncMap[time.Duration, *time.Timer]
	Id       int
	Title    string
	Author   string
	Create   CustomTime
	MaxPage  int
	MaxFloor int
	Metadata *Metadata
	Result   DownResult
}

func NewTopic(root string, id int) *Topic {
	return &Topic{
		root:     root,
		timers:   NewSyncMap[time.Duration, *time.Timer](),
		Id:       id,
		Metadata: &Metadata{},
	}
}

func LoadTopic(root string, id int) (*Topic, error) {
	dir := filepath.Join(filepath.Clean(root), strconv.Itoa(id))
	log.Printf("从 %s 加载帖子\n", dir)

	topic := NewTopic(dir, id)

	content := filepath.Join(dir, POST_MARKDOWN)
	if IsExist(content) {
		re := regexp.MustCompile(`\\<pid:0\\>\s+(\d{4}-\d{2}-\d{2}\s+\d{2}:\d{2}:\d{2})\s+by\s+(.+)\s*<`)
		e := ReadLine(content, func(line string, i int) bool {
			if i == 0 {
				topic.Title = strings.TrimLeft(line, "# ")
			} else {
				m := re.FindStringSubmatch(line)
				if m != nil {
					t, e := time.Parse("2006-01-02 15:04:05", m[1])
					if e != nil {
						log.Println("解析时间失败:", m[1], e)
						return false
					}

					topic.Create = FromTime(t)
					topic.Author = m[2]
					return false
				}
			}
			return true
		})
		if e != nil {
			return nil, e
		}
		if topic.Title == "" {
			log.Printf("帖子 %d 中未找到标题", id)
		}
	} else {
		log.Printf("在目录 %s 中未找到 %s", dir, POST_MARKDOWN)
	}

	pd, e := ini.Load(filepath.Join(dir, PROCESS_INI))
	if e == nil {
		sec := pd.Section("local")
		topic.MaxPage = sec.Key("max_page").MustInt(1)
		topic.MaxFloor = sec.Key("max_floor").MustInt(-1)
	}

	md := new(Metadata)
	jd, e := os.ReadFile(filepath.Join(dir, METADATA_JSON))
	if e != nil {
		if os.IsNotExist(e) {
			log.Println("未找到帖子的元数据", id)
		} else {
			log.Println("读取帖子元数据失败:", e)
		}
	} else if e := json.Unmarshal(jd, md); e != nil {
		log.Println("解析帖子元数据失败:", e)
	}
	topic.Metadata = md

	topic.Modify()

	return topic, nil
}

func (t *Topic) Save() error {
	dir := t.root

	os.MkdirAll(dir, 0755)

	md := filepath.Join(dir, METADATA_JSON)
	data, e := json.MarshalIndent(t.Metadata, "", "  ")
	if e != nil {
		return e
	}
	return os.WriteFile(md, data, 0644)
}

func (t *Topic) Content() (string, error) {
	md := filepath.Join(t.root, POST_MARKDOWN)
	data, e := os.ReadFile(md)
	if e != nil {
		return "", e
	}
	return string(data), nil
}

func (t *Topic) Stop() {
	t.timers.EAC(func(_ time.Duration, timer *time.Timer) {
		timer.Stop()
	})
}

func (t *Topic) Modify() {
	t.modAt = Now()
}
