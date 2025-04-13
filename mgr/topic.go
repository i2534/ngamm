package mgr

import (
	"encoding/json"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
	"gopkg.in/ini.v1"
)

type Metadata struct {
	UpdateCron    string
	updateCronId  cron.EntryID
	MaxRetryCount int
	retryCount    int
	Abandon       bool // 已达到最大重试次数, 放弃更新
	mutex         *sync.Mutex
}

func NewMetadata() *Metadata {
	return &Metadata{
		mutex: &sync.Mutex{},
	}
}

func (m *Metadata) Merge(n *Metadata) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.UpdateCron = n.UpdateCron
	m.MaxRetryCount = n.MaxRetryCount
	m.Abandon = n.Abandon
}

type DownResult struct {
	Success bool
	Message string
	Time    CustomTime
}

type Topic struct {
	root     *ExtRoot
	modAt    CustomTime
	timers   *SyncMap[time.Duration, *time.Timer]
	Id       int
	Title    string
	Author   string
	Uid      int // 用户 ID
	Create   CustomTime
	MaxPage  int
	MaxFloor int
	Metadata *Metadata
	Result   DownResult
}

func NewTopic(root *ExtRoot, id int) *Topic {
	return &Topic{
		root:     root,
		timers:   NewSyncMap[time.Duration, *time.Timer](),
		Id:       id,
		Metadata: NewMetadata(),
	}
}

var (
	regexAuthorInfo  *regexp.Regexp = regexp.MustCompile(`\\<pid:0\\>\s+(\d{4}-\d{2}-\d{2}\s+\d{2}:\d{2}:\d{2})\s+by\s+([^(]+)(\(\d+\))?\s*<`)
	regexAuthorIsUID *regexp.Regexp = regexp.MustCompile(`UID(\d+)`)
)

func LoadTopic(root *ExtRoot, id int, nga *Client) (*Topic, error) {
	dir, e := root.SafeOpenRoot(strconv.Itoa(id))
	if e != nil {
		return nil, e
	}

	log.Printf("从 %s 加载帖子\n", dir.Name())

	topic := NewTopic(dir, id)

	if dir.IsExist(POST_MARKDOWN) {
		if e := dir.EveryLine(POST_MARKDOWN, func(line string, i int) bool {
			if i == 0 {
				topic.Title = strings.TrimLeft(line, "# ")
			} else if strings.HasPrefix(line, "#####") {
				m := regexAuthorInfo.FindStringSubmatch(line)
				if m != nil {
					t, e := time.Parse("2006-01-02 15:04:05", m[1])
					if e != nil {
						log.Println("解析时间失败:", m[1], e)
						return false
					}

					topic.Create = FromTime(t)
					topic.Author = m[2]

					if len(m) > 3 {
						val := m[3]
						if val != "" {
							val = val[1 : len(val)-1]
						}
						if val != "" {
							uid, e := strconv.Atoi(val)
							if e != nil {
								log.Println("解析用户 ID 失败:", val, e)
							} else {
								topic.Uid = uid
							}
						}
					}

					if topic.Uid == 0 { // 1.6.0 及之前的版本没有 UID
						go func() {
							if u, e := nga.GetUser(topic.Author); e != nil {
								log.Println("获取用户信息失败:", topic.Author, e)
							} else {
								topic.Author = u.Name
								topic.Uid = u.Id
							}
						}()
					} else if regexAuthorIsUID.MatchString(topic.Author) { // 部分用户的名称是 UIDxxxx, 用 uid 拼接出来的
						m := regexAuthorIsUID.FindStringSubmatch(topic.Author)
						if m != nil {
							uid, e := strconv.Atoi(m[1])
							if e != nil {
								log.Println("解析用户 ID 失败:", m[1], e)
							} else {
								go func() {
									if u, e := nga.GetUserById(uid); e != nil {
										log.Println("获取用户信息失败:", topic.Author, e)
									} else {
										topic.Author = u.Name
										topic.Uid = u.Id
									}
								}()
							}
						}
					}

					return false
				}
			}
			return true
		}); e != nil {
			return nil, e
		}
		if topic.Title == "" {
			log.Printf("帖子 %d 中未找到标题", id)
		} else {
			log.Printf("成功加载帖子 %d : <%s>", id, topic.Title)
		}
	} else {
		log.Printf("在目录 %s 中未找到 %s", dir.Name(), POST_MARKDOWN)
	}

	pd, e := dir.ReadAll(PROCESS_INI)
	if e != nil {
		return nil, e
	}
	pi, e := ini.Load(pd)
	if e == nil {
		sec := pi.Section("local")
		topic.MaxPage = sec.Key("max_page").MustInt(1)
		topic.MaxFloor = sec.Key("max_floor").MustInt(-1)
	}

	md := NewMetadata()
	jd, e := dir.ReadAll(METADATA_JSON)
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

func (t *Topic) SaveMeta() error {
	m := t.Metadata
	m.mutex.Lock()
	defer m.mutex.Unlock()

	data, e := json.MarshalIndent(m, "", "  ")
	if e != nil {
		return e
	}
	return t.root.WriteAll(METADATA_JSON, data)
}

func (t *Topic) Content() (string, error) {
	data, e := t.root.ReadAll(POST_MARKDOWN)
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
func (t *Topic) Close() error {
	t.Stop()
	return t.root.Close()
}

func (t *Topic) Modify() {
	t.modAt = Now()
}
