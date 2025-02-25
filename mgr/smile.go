package mgr

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync/atomic"
)

type item struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Prefix string `json:"prefix"`
	data   atomic.Value
}
type Smile struct {
	Base string `json:"base"`
	List []item `json:"list"`

	root   *ExtRoot
	cache  *SyncMap[string, *item]
	failed *SyncMap[string, bool]
}

func Unmarshal(data []byte) (*Smile, error) {
	smile := &Smile{}
	if e := json.Unmarshal(data, smile); e != nil {
		return nil, e
	}
	smile.cache = NewSyncMap[string, *item]()
	smile.failed = NewSyncMap[string, bool]()
	return smile, nil
}

func (s *Smile) find(name string) *item {
	if v, has := s.cache.Get(name); has {
		return v
	}

	i := strings.LastIndex(name, "/")
	var n string
	if i == -1 {
		n = name
	} else {
		n = name[:i]
	}
	for _, v := range s.List {
		b := v.Path == name //完全匹配路径
		if !b {
			if v.Prefix == "" { //无前缀, 完全匹配名称
				b = v.Name == n
			} else { //前缀匹配且后缀匹配名称
				b = strings.HasPrefix(n, v.Prefix) && strings.HasSuffix(n, v.Name)
			}
		}

		if b {
			s.cache.Put(name, &v)
			return &v
		}
	}
	return nil
}

func (s *Smile) IsPath(name string) bool {
	for _, v := range s.List {
		if v.Path == name {
			return true
		}
	}
	return false
}

func (s *Smile) URL(name string) string {
	v := s.find(name)
	if v == nil {
		return ""
	}
	return s.Base + v.Path
}

func (s *Smile) Local(name, ua string) ([]byte, error) {
	v := s.find(name)
	if v == nil {
		return nil, fmt.Errorf("未找到表情 %s", name)
	}
	d := v.data.Load()
	if d != nil {
		return d.([]byte), nil
	}

	if s.root.IsExist(v.Path) {
		data, e := s.root.ReadAll(v.Path)
		if e != nil {
			return nil, fmt.Errorf("读取表情 %s 失败: %w", name, e)
		}
		v.data.Store(data)
		return data, nil
	}

	if s.failed.Has(name) {
		return nil, fmt.Errorf("下载表情 %s 失败", name)
	}

	url := s.URL(name)
	if url != "" {
		go func() {
			if e := s.fetch(v.Path, url, ua); e != nil {
				log.Println(e.Error())
			} else {
				s.failed.Put(name, true)
			}
		}()
	}
	return nil, nil
}

func (s *Smile) fetch(path, url, ua string) error {
	req, e := http.NewRequest(http.MethodGet, url, nil)
	if e != nil {
		return fmt.Errorf("创建请求失败: %w", e)
	}
	req.Header.Set("User-Agent", ua)

	resp, e := DoHttp(req)
	if e != nil {
		return fmt.Errorf("下载表情 %s 失败: %w", url, e)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载表情 %s 失败, 状态码: %d", url, resp.StatusCode)
	}

	file, e := s.root.Create(path)
	if e != nil {
		return fmt.Errorf("创建表情文件失败: %w", e)
	}
	defer file.Close()

	_, e = io.Copy(file, resp.Body)
	if e != nil {
		return fmt.Errorf("保存表情图片失败: %w", e)
	}
	log.Printf("下载表情 %s 到 %s\n", url, path)
	return nil
}
