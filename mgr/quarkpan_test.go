package mgr_test

import (
	"testing"
	"time"

	"github.com/i2534/ngamm/mgr"
	"gopkg.in/ini.v1"
)

func testGetQuarkCookie(t *testing.T) string {
	cfg, e := ini.Load("../data/pan/config.ini")
	if e != nil {
		t.Fatalf("Failed to load config: %v", e)
	}
	cookie := cfg.Section("quark").Key("cookie").String()
	if cookie == "" {
		t.Fatal("Quark cookie is empty")
	}
	return cookie
}

func testInitQuarkPan(t *testing.T) *mgr.QuarkPan {
	cookie := testGetQuarkCookie(t)
	quark := mgr.NewQuarkPan(mgr.QuarkCfg{
		Root:   "../data/pan/quark",
		Cookie: cookie,
	})

	if err := quark.Init(); err != nil {
		t.Fatalf("Failed to initialize Quark: %v", err)
	}
	return quark
}

func TestQuarkDelete(t *testing.T) {
	cookie := testGetQuarkCookie(t)
	quark := mgr.NewQuark(cookie)
	quark.Init()
	getFids := quark.GetFids([]string{"/test/aaaa"})
	f0 := getFids[0]
	t.Logf("Fid: %+v", f0)
	r := quark.Delete([]string{f0["fid"].(string)})
	t.Logf("Result: %+v", r)
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
