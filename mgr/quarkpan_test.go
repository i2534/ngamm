package mgr_test

import (
	"testing"
	"time"

	"github.com/i2534/ngamm/mgr"
	"gopkg.in/ini.v1"
)

func testInitQuarkPan(t *testing.T) *mgr.QuarkPan {
	cfg, e := ini.Load("/workspaces/ngamm/ngapost2md/quark.ini")
	if e != nil {
		t.Fatalf("Failed to load config: %v", e)
	}
	cookie := cfg.Section("").Key("cookie").String()
	quark := mgr.NewQuarkPan(mgr.QuarkCfg{
		Root:   "/workspaces/ngamm/ngapost2md",
		Cookie: cookie,
	})

	if err := quark.Init(); err != nil {
		t.Fatalf("Failed to initialize Quark: %v", err)
	}
	return quark
}

func TestQuarkInit(t *testing.T) {
	quark := testInitQuarkPan(t)
	defer quark.Close()
}

func TestQuarkTransfer(t *testing.T) {
	quark := testInitQuarkPan(t)
	defer quark.Close()

	if err := quark.Transfer(0, mgr.PanMetadata{
		URL: "https://pan.quark.cn/s/f8e65247ffaa",
		Tqm: "k19c",
	}); err != nil {
		t.Fatalf("Failed to transfer file: %v", err)
	}

	if err := quark.Transfer(0, mgr.PanMetadata{
		URL: "https://pan.quark.cn/s/cfa69cc91b16?pwd=shhh",
	}); err != nil {
		t.Fatalf("Failed to transfer file: %v", err)
	}

	time.Sleep(time.Second * 5)
}
