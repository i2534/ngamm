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
	"sync"
)

type item struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Prefix string `json:"prefix"`
}
type Smile struct {
	Base string `json:"base"`
	List []item `json:"list"`

	root   string
	cache  sync.Map
	failed sync.Map
}

func Unmarshal(data []byte) (*Smile, error) {
	smile := &Smile{}
	if e := json.Unmarshal(data, smile); e != nil {
		return nil, e
	}
	return smile, nil
}

func (s *Smile) find(name string) *item {
	if v, has := s.cache.Load(name); has {
		return v.(*item)
	}

	n := name[:strings.LastIndex(name, ".")]
	for _, v := range s.List {
		b := v.Path == name //完全匹配路径
		if !b {
			if v.Prefix == "" { // 无前缀, 完全匹配名称 TODO: 此处可能有问题, 因为基础表情基本没人用
				b = v.Name == n
			} else { //前缀匹配且后缀匹配名称
				b = strings.HasPrefix(n, v.Prefix) && strings.HasSuffix(n, v.Name)
			}
		}

		if b {
			s.cache.Store(name, &v)
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

func (s *Smile) Local(name, ua string) string {
	v := s.find(name)
	if v == nil {
		return ""
	}
	path := filepath.Join(s.root, v.Path)
	if IsExist(path) {
		return path
	}

	if _, ok := s.failed.Load(name); ok {
		return ""
	}

	url := s.URL(name)
	if url != "" {
		go func() {
			if e := s.fetch(path, url, ua); e != nil {
				log.Println(e.Error())
			} else {
				s.failed.Store(name, true)
			}
		}()
	}
	return ""
}

func (s *Smile) fetch(path, url, ua string) error {
	req, e := http.NewRequest(http.MethodGet, url, nil)
	if e != nil {
		return fmt.Errorf("failed to create request: %w", e)
	}
	req.Header.Set("User-Agent", ua)

	client := http.Client{}
	resp, e := client.Do(req)
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
