package mgr

import (
	"os"
	"testing"

	"github.com/go-playground/assert/v2"
)

var smile *Smile

func setup() {
	data, e := os.ReadFile("assets/smiles.json")
	if e != nil {
		panic(e)
	}
	smile, e = Unmarshal(data)
	if e != nil {
		panic(e)
	}
}
func teardown() {

}

func TestMain(m *testing.M) {
	setup()
	code := m.Run()
	teardown()
	os.Exit(code)
}

func TestLocal(t *testing.T) {
	assert.NotEqual(t, smile, nil)
	assert.NotEqual(t, smile.find("ac18.png"), nil)
	assert.NotEqual(t, smile.find("ng_奸笑"), nil)
	assert.NotEqual(t, smile.find("crazy.gif"), nil)
	assert.NotEqual(t, smile.find("1"), nil)
}
