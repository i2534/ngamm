package mgr_test

import (
	"testing"
	"time"

	"github.com/i2534/ngamm/mgr"
)

func initBaidu(t *testing.T, dir ...string) *mgr.Baidu {
	saveRoot := ""
	if len(dir) > 0 {
		saveRoot = dir[0]
	}
	baidu := mgr.NewBaidu(mgr.BaiduCfg{
		Root:         "../data/pan/baidu",
		TransferDest: saveRoot,
	})
	if err := baidu.Init(); err != nil {
		t.Fatalf("Failed to initialize Baidu: %v", err)
	}
	return baidu
}

func TestBaiduInit(t *testing.T) {
	baidu := initBaidu(t)
	defer baidu.Close()
}

func TestBaiduLs(t *testing.T) {
	baidu := initBaidu(t)
	defer baidu.Close()

	if ns, err := baidu.Ls("/我的资源"); err != nil {
		t.Fatalf("Failed to list files: %v", err)
	} else {
		for _, n := range ns {
			t.Logf("File: %+v", n)
		}
	}
}

func TestBaiduTransfer(t *testing.T) {
	baidu := initBaidu(t, "/MyTransfer")
	defer baidu.Close()

	if err := baidu.Transfer(0, mgr.TransferRecord{
		URL: "https://pan.baidu.com/s/1i0Voz5PwgB-TX9xm5-5-Ng?pwd=534h?",
	}); err != nil {
		t.Fatalf("Failed to transfer files: %v", err)
	}

	time.Sleep(5 * time.Second)
}

func TestBaiduMove(t *testing.T) {
	baidu := initBaidu(t)
	defer baidu.Close()
	if err := baidu.Move(45490866); err != nil {
		t.Fatalf("Failed to move files: %v", err)
	}
}
