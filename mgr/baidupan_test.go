package mgr_test

import (
	"testing"
	"time"

	"github.com/i2534/ngamm/mgr"
)

func TestBaiduInit(t *testing.T) {
	baidu := mgr.NewBaidu(mgr.BaiduCfg{
		Root: "/workspaces/ngamm/ngapost2md/BaiduPCS-Go",
	})
	defer baidu.Close()

	if err := baidu.Init(); err != nil {
		t.Fatalf("Failed to initialize Baidu: %v", err)
	}
}

func TestBaiduTransfer(t *testing.T) {
	baidu := mgr.NewBaidu(mgr.BaiduCfg{
		Root: "/workspaces/ngamm/ngapost2md/BaiduPCS-Go",
	})
	defer baidu.Close()

	baidu.Init()

	// baidu.Upload("/workspaces/ngamm/LICENSE", "/我的资源/43800012")

	if err := baidu.Transfer(43808300, mgr.PanMetadata{
		URL: "https://pan.baidu.com/s/1i0Voz5PwgB-TX9xm5-5-Ng?pwd=534h?",
	}); err != nil {
		t.Fatalf("Failed to transfer files: %v", err)
	}

	time.Sleep(5 * time.Second)
}
