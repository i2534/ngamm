package mgr

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/i2534/ngamm/mgr/log"
	"github.com/robfig/cron/v3"
	"gopkg.in/ini.v1"
)

var (
	groupTopic = log.GROUP_TOPIC
)

type Metadata struct {
	updateCronId  cron.EntryID
	MaxRetryCount int
	retryCount    int
	UpdateCron    string
	mutex         *sync.Mutex
	Abandon       bool // 已达到最大重试次数, 放弃更新
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
	timers   *SyncMap[time.Duration, *time.Timer]
	Id       int
	Uid      int // 用户 ID
	MaxPage  int
	MaxFloor int
	Metadata *Metadata
	Title    string
	Author   string
	Create   CustomTime
	modAt    CustomTime
	Result   DownResult
	closed   bool // 是否已关闭
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
	regexImageURL    *regexp.Regexp = regexp.MustCompile(`!\[img\]\(([^)]+)\)`)
	regexVideoURL    *regexp.Regexp = regexp.MustCompile(`<video[^>]*\s+src="([^"]+)"[^>]*\s+poster="([^"]+)"`)
)

func (topic *Topic) parse(nga *Client) error {
	return topic.root.EveryLine(POST_MARKDOWN, func(line string, i int) bool {
		if i == 0 {
			topic.Title = strings.TrimLeft(line, "# ")
		} else if strings.HasPrefix(line, "#####") {
			m := regexAuthorInfo.FindStringSubmatch(line)
			if m != nil {
				t, e := time.Parse("2006-01-02 15:04:05", m[1])
				if e != nil {
					log.Group(groupTopic).Println("解析时间失败:", m[1], e)
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
							log.Group(groupTopic).Println("解析用户 ID 失败:", val, e)
						} else {
							topic.Uid = uid
						}
					}
				}

				if topic.Uid == 0 { // 1.6.0 及之前的版本没有 UID
					go func() {
						if u, e := nga.GetUser(topic.Author); e != nil {
							log.Group(groupTopic).Println("获取用户信息失败:", topic.Author, e)
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
							log.Group(groupTopic).Println("解析用户 ID 失败:", m[1], e)
						} else {
							go func() {
								if u, e := nga.GetUserById(uid); e != nil {
									log.Group(groupTopic).Println("获取用户信息失败:", topic.Author, e)
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
	})
}

func LoadTopic(root *ExtRoot, id int, nga *Client) (*Topic, error) {
	dir, e := root.SafeOpenRoot(strconv.Itoa(id))
	if e != nil {
		return nil, e
	}

	log.Group(groupTopic).Printf("从 %s 加载帖子\n", dir.Name())

	topic := NewTopic(dir, id)

	if dir.IsExist(POST_MARKDOWN) {
		if e := topic.parse(nga); e != nil {
			log.Group(groupTopic).Printf("解析帖子 %d 失败: %s", id, e)
			return nil, e
		}
		if topic.Title == "" {
			log.Group(groupTopic).Printf("帖子 %d 中未找到标题", id)
		} else {
			log.Group(groupTopic).Printf("成功加载帖子 %d : <%s>", id, topic.Title)
		}
	} else {
		log.Group(groupTopic).Printf("在目录 %s 中未找到 %s", dir.Name(), POST_MARKDOWN)
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
		if !os.IsNotExist(e) {
			log.Group(groupTopic).Println("读取帖子元数据失败:", e)
		}
	} else if e := json.Unmarshal(jd, md); e != nil {
		log.Group(groupTopic).Println("解析帖子元数据失败:", e)
	}
	topic.Metadata = md

	topic.Modify()

	nga.SetTopicUser(topic.Uid, topic.Author)

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
	t.Metadata.mutex.Lock()
	defer t.Metadata.mutex.Unlock()

	if t.closed {
		return nil
	}
	t.closed = true
	t.Stop()
	return t.root.Close()
}

func (t *Topic) Modify() {
	t.modAt = Now()
}

func (t *Topic) IsClosed() bool {
	return t.closed
}
func (t *Topic) fixInvalidAssets(nga *Client) {
	if !t.root.IsExist(ASSETSA_JSON) {
		return
	}
	t.Metadata.mutex.Lock()
	data, e := t.root.ReadAll(ASSETSA_JSON)
	t.Metadata.mutex.Unlock()

	if e != nil {
		log.Group(groupTopic).Println("读取 assets.json 失败:", e)
		return
	}

	m := make(map[string]string)
	if e := json.Unmarshal(data, &m); e != nil {
		log.Group(groupTopic).Println("解析 assets.json 失败:", e)
		return
	}

	fixes := make(map[string]string)
	for k, v := range m {
		i := strings.Index(k, "_")
		if i != -1 {
			if _, e := strconv.Atoi(k[:i]); e == nil {
				f, e := t.root.OpenReader(k)
				if e != nil {
					log.Group(groupTopic).Printf("打开 %s 失败: %v\n", k, e)
					continue
				}
				defer f.Close()

				buf := make([]byte, 1024)
				n, e := f.Read(buf)
				if e != nil && e != io.EOF {
					log.Group(groupTopic).Printf("读取 %s 失败: %v\n", k, e)
					continue
				}
				if IsVaildImage(buf[:n]) {
					log.Group(groupTopic).Printf("文件 %s 不是图片, 需要处理\n", k)
					fixes[k] = v
				}
			}
		}
	}

	if nga != nil && len(fixes) > 0 {
		nga.AddFixAsset(t, fixes)
	}
}
func (t *Topic) TryFetchAssets(nga *Client) {
	as := make(map[string]struct{})
	t.root.EveryLine(POST_MARKDOWN, func(line string, i int) bool {
		ims := regexImageURL.FindAllStringSubmatch(line, -1)
		for _, m := range ims {
			url := m[1]
			if strings.HasPrefix(url, ATTACHMENT_BASE) {
				as[url] = struct{}{}
			}
		}
		vms := regexVideoURL.FindAllStringSubmatch(line, -1)
		for _, m := range vms {
			src := m[1]
			poster := m[2]
			if strings.HasPrefix(src, ATTACHMENT_BASE) {
				as[src] = struct{}{}
			}
			if strings.HasPrefix(poster, ATTACHMENT_BASE) {
				as[poster] = struct{}{}
			}
		}
		return true
	})
	// fmt.Printf("%v", as)

	fixes := make(map[string]string)
	for url := range as {
		hash := ShortSha1(url)
		name := ATTACH_DIR + "/" + hash + filepath.Ext(url)

		if t.root.IsExist(name) {
			// log.Group(groupTopic).Printf("文件 %s 已存在, 跳过\n", name)
			continue
		}

		fixes[name] = url
	}
	if nga != nil && len(fixes) > 0 {
		nga.AddFixAsset(t, fixes)
	}
}
func (t *Topic) TryFixAssets(nga *Client) {
	if nga == nil {
		return
	}
	if !nga.attachCfg.Base.AutoDown {
		log.Group(groupTopic).Printf("自动下载附件已禁用, 跳过 %d 的附件下载\n", t.Id)
		return
	}

	t.fixInvalidAssets(nga)

	if nga.IsUseNetworkPic() {
		t.TryFetchAssets(nga)
	}
}
