package mgr

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
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

	root   string
	cache  *SyncMap[string, *item]
	failed *SyncMap[string, bool]
	client *http.Client
}

func Unmarshal(data []byte) (*Smile, error) {
	smile := &Smile{}
	if e := json.Unmarshal(data, smile); e != nil {
		return nil, e
	}
	smile.client = &http.Client{
		Timeout: 10 * time.Second,
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
		return nil, fmt.Errorf("smile %s not found", name)
	}
	d := v.data.Load()
	if d != nil {
		return d.([]byte), nil
	}
	path := filepath.Join(s.root, v.Path)
	if IsExist(path) {
		data, e := os.ReadFile(path)
		if e != nil {
			return nil, fmt.Errorf("failed to read smile %s: %w", name, e)
		}
		v.data.Store(data)
		return data, nil
	}

	if s.failed.Has(name) {
		return nil, fmt.Errorf("failed to download smile %s", name)
	}

	url := s.URL(name)
	if url != "" {
		go func() {
			if e := s.fetch(path, url, ua); e != nil {
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
		return fmt.Errorf("failed to create request: %w", e)
	}
	req.Header.Set("User-Agent", ua)

	resp, e := s.client.Do(req)
	if e != nil {
		return fmt.Errorf("failed to download smile %s: %w", url, e)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download smile %s: status code %d", url, resp.StatusCode)
	}

	os.MkdirAll(s.root, os.ModePerm)
	file, e := os.Create(path)
	if e != nil {
		return fmt.Errorf("failed to create file: %w", e)
	}
	defer file.Close()

	_, e = io.Copy(file, resp.Body)
	if e != nil {
		return fmt.Errorf("failed to save image: %w", e)
	}
	log.Printf("Downloaded smile %s to %s\n", url, path)
	return nil
}
