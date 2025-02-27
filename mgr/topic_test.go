package mgr_test

import (
	"regexp"
	"testing"
)

func TestParseAuthor(t *testing.T) {
	re := regexp.MustCompile(`\\<pid:0\\>\s+(\d{4}-\d{2}-\d{2}\s+\d{2}:\d{2}:\d{2})\s+by\s+([^(]+)(\(\d+\))?\s*<`)
	author := `##### <span id="pid0">0.[0] \<pid:0\> 2025-02-27 03:02:58 by lukaka_1901(66412753)</span>`
	m := re.FindStringSubmatch(author)
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
	m = re.FindStringSubmatch(author)
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
