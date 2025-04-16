package mgr

import (
	"fmt"

	"gopkg.in/ini.v1"
)

type Config struct {
	Port      int
	Program   string
	Smile     string
	Token     string
	Pan       string
	TopicRoot string
}

func LoadConfig(path string) (*Config, error) {
	cfg := &Config{
		Port:    5842,
		Program: "ngapost2md/ngapost2md",
		Smile:   "local",
	}
	if path == "" {
		return nil, fmt.Errorf("配置文件路径不能为空")
	}
	c, e := ini.Load(path)
	if e != nil {
		return nil, e
	}
	c.BlockMode = true
	e = c.MapTo(cfg)
	return cfg, e
}
