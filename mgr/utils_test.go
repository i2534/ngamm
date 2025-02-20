package mgr_test

import (
	"testing"

	"github.com/go-playground/assert/v2"
	"github.com/i2534/ngamm/mgr"
)

func TestRootReadDir(t *testing.T) {
	root, e := mgr.OpenRoot(".")
	if e != nil {
		t.Fatal(e)
	}
	defer root.Close()

	if es, e := root.ReadDir("."); e != nil {
		t.Fatal(e)
	} else {
		t.Log(es)
		assert.NotEqual(t, len(es), 0)
	}
	_, e = root.ReadDir("xxx")
	t.Log(e)
	assert.NotEqual(t, e, nil)
	_, e = root.ReadDir("..")
	t.Log(e)
	assert.NotEqual(t, e, nil)
}
func TestRootAbsPath(t *testing.T) {
	root, e := mgr.OpenRoot(".")
	if e != nil {
		t.Fatal(e)
	}
	defer root.Close()

	if es, e := root.ReadDir("."); e != nil {
		t.Fatal(e)
	} else {
		for _, e := range es {
			if e.IsDir() {
				continue
			}
			if p, e := root.AbsPath(e.Name()); e != nil {
				t.Fatal(e)
			} else {
				t.Log(p)
				assert.NotEqual(t, p, "")
			}
		}
	}
}
func TestRootIsExist(t *testing.T) {
	root, e := mgr.OpenRoot(".")
	if e != nil {
		t.Fatal(e)
	}
	defer root.Close()

	if es, e := root.ReadDir("."); e != nil {
		t.Fatal(e)
	} else {
		for _, e := range es {
			if e.IsDir() {
				continue
			}
			if e := root.IsExist(e.Name()); !e {
				t.Fatal(e)
			}
		}
	}

	if e := root.IsExist("xxx"); e {
		t.Fatal(e)
	}
}
