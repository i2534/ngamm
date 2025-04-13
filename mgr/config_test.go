package mgr_test

import (
	"path/filepath"
	"testing"

	"github.com/i2534/ngamm/mgr"
)

func TestLoadConfig(t *testing.T) {
	cp := filepath.Join("/workspaces/ngamm", "/config.ini")
	println(cp)
	cfg, e := mgr.LoadConfig(cp)
	if e != nil {
		t.Fatalf("加载配置文件失败: %s", e.Error())
	}

	if cfg.Port != 5842 {
		t.Fatalf("端口号错误, 期望: 5842, 实际: %d", cfg.Port)
	}
	if cfg.Program != "ngapost2md/ngapost2md" {
		t.Fatalf("程序路径错误, 期望: ngapost2md/ngapost2md, 实际: %s", cfg.Program)
	}
	if cfg.Token != "" {
		t.Fatalf("令牌错误, 期望: '', 实际: %s", cfg.Token)
	}
	if cfg.Smile != "local" {
		t.Fatalf("表情配置错误, 期望: local, 实际: %s", cfg.Smile)
	}
	println(cfg.Baidu.ReleaseURL)
	cfg.Baidu.ReleaseURL = "xxx"
	println(cfg.Baidu.ReleaseURL)
}
