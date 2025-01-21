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

type CustomTime struct {
	time.Time
}

func FromTime(t time.Time) CustomTime {
	return CustomTime{Time: t}
}
func Now() CustomTime {
	return FromTime(time.Now())
}
func (t CustomTime) MarshalJSON() ([]byte, error) {
	if t.IsZero() {
		return []byte(`""`), nil
	}
	lt := t.In(TIME_LOC)
	return json.Marshal(lt.Format("2006-01-02 15:04:05"))
}

type DownResult struct {
	Success bool
	Message string
	Time    CustomTime
}

type Topic struct {
	root     string
	loadAt   CustomTime
	Id       int
	Title    string
	Author   string
	Create   CustomTime
	MaxPage  int
	MaxFloor int
	Metadata *Metadata
	Result   DownResult
}

func LoadTopic(root string, id int) (*Topic, error) {
	dir := filepath.Join(filepath.Clean(root), strconv.Itoa(id))
	log.Printf("Loading topic %d from %s\n", id, dir)

	topic := &Topic{
		root: dir,
		Id:   id,
	}

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
						log.Println("Failed to parse time:", m[1], e)
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
			log.Printf("Not found title in  %s at dir: %s", POST_MARKDOWN, dir)
		}
	} else {
		log.Printf("Not found %s at dir: %s", POST_MARKDOWN, dir)
	}

	pd, e := ini.Load(filepath.Join(dir, PROCESS_INI))
	if e == nil {
		sec := pd.Section("local")
		topic.MaxPage = sec.Key("max_page").MustInt(0)
		topic.MaxFloor = sec.Key("max_floor").MustInt(0)
	}

	md := new(Metadata)
	jd, e := os.ReadFile(filepath.Join(dir, METADATA_JSON))
	if e != nil {
		if os.IsNotExist(e) {
			log.Println("No metadata found for topic", id)
		} else {
			log.Println("Failed to read metadata:", e)
		}
	} else if e := json.Unmarshal(jd, md); e != nil {
		log.Println("Failed to parse metadata:", e)
	}
	topic.Metadata = md

	topic.loadAt = Now()

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
