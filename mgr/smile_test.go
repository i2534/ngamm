package mgr_test

import (
	"os"
	"testing"

	"github.com/go-playground/assert/v2"
	"github.com/i2534/ngamm/mgr"
)

var smile *mgr.Smile

func getSmile() *mgr.Smile {
	if smile != nil {
		return smile
	}

	data, e := os.ReadFile("assets/smiles.json")
	if e != nil {
		panic(e)
	}
	smile, e = mgr.Unmarshal(data)
	if e != nil {
		panic(e)
	}
	return smile
}

func TestLocal(t *testing.T) {
	assert.NotEqual(t, getSmile(), nil)
}
