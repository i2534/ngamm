package mgr_test

import (
	"regexp"
	"testing"

	"github.com/i2534/ngamm/mgr"
)

var (
	regexAuthorInfo  *regexp.Regexp = regexp.MustCompile(`\\<pid:0\\>\s+(\d{4}-\d{2}-\d{2}\s+\d{2}:\d{2}:\d{2})\s+by\s+([^(]+)(\(\d+\))?\s*<`)
	regexAuthorIsUID *regexp.Regexp = regexp.MustCompile(`UID(\d+)`)
)

func TestParseAuthor(t *testing.T) {
	author := `##### <span id="pid0">0.[0] \<pid:0\> 2025-02-27 03:02:58 by lukaka_1901(66412753)</span>`
	m := regexAuthorInfo.FindStringSubmatch(author)
	if m == nil {
		t.Error("Parse author failed")
	}
	t.Log(m[1:])
	if m[2] != "lukaka_1901" {
		t.Error("Parse author failed")
	}
	if m[3] != "(66412753)" {
		t.Error("Parse author failed")
	}

	author = `##### <span id="pid0">0.[0] \<pid:0\> 2025-02-20 05:41:28 by 慕容巽</span>`
	m = regexAuthorInfo.FindStringSubmatch(author)
	if m == nil {
		t.Error("Parse author failed")
	}
	t.Log(m[1:])
	if m[2] != "慕容巽" {
		t.Error("Parse author failed")
	}
	if m[3] != "" {
		t.Error("Parse author failed")
	}
}

func TestIsUID(t *testing.T) {
	if !regexAuthorIsUID.MatchString("UID66596428") {
		t.Error("Is UID failed")
	} else {
		t.Log(regexAuthorIsUID.FindStringSubmatch("UID66596428"))
	}
	if regexAuthorIsUID.MatchString("lukaka_1901") {
		t.Error("Is UID failed")
	}
}

func TestParseTransferRecord(t *testing.T) {
	dir, e := mgr.OpenRoot("../data/43894008")
	if e != nil {
		t.Error("Open root failed:", e)
		return
	}
	topic := mgr.NewTopic(dir, 0)
	if m, e := topic.ParseTransferRecord(); e != nil {
		t.Error("Get pan metadata failed:", e)
	} else {
		t.Logf("Get pan metadata: %+v", m)
	}
}

func TestParseTransferRecord2(t *testing.T) {
	dir, e := mgr.OpenRoot("../data/43832556")
	if e != nil {
		t.Error("Open root failed:", e)
		return
	}
	topic := mgr.NewTopic(dir, 0)
	if m, e := topic.ParseTransferRecord(); e != nil {
		t.Error("Get pan metadata failed:", e)
	} else {
		t.Logf("Get pan metadata: %+v", m)
	}
}

func TestTryTransfer(t *testing.T) {
	dir, e := mgr.OpenRoot("../data/43809567")
	if e != nil {
		t.Error("Open root failed:", e)
		return
	}
	topic := mgr.NewTopic(dir, 0)
	topic.AutoTransfer(nil)
}
