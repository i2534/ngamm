package mgr_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/go-playground/assert/v2"
	"github.com/i2534/ngamm/mgr"
)

var nga *mgr.Client

func getNga() *mgr.Client {
	if nga != nil {
		return nga
	}
	wd, e := os.Getwd()
	if e != nil {
		panic(e)
	}
	np := filepath.Join(wd, "../data/np2md/ngapost2md")
	td := os.Getenv("TOPIC_ROOT")
	nga, e = mgr.InitNGA(mgr.Config{
		Program:   np,
		TopicRoot: td,
	})
	if e != nil {
		panic(e)
	}
	return nga
}

func TestGetUA(t *testing.T) {
	ua := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/129.0.0.0 Safari/537.36 Edg/129.0.0.0"

	nga := getNga()
	defer nga.Close()

	assert.Equal(t, nga.GetUA(), ua)
}

func TestDownTopic(t *testing.T) {
	os.Setenv("TOPIC_ROOT", "./topics")

	nga := getNga()
	defer nga.Close()

	id := 43833908
	b, v := nga.DownTopic(id)
	assert.Equal(t, b, true)
	t.Log(v)
}

func TestGetUser(t *testing.T) {
	user, e := getNga().GetUser("aot10086")
	assert.Equal(t, e, nil)
	assert.Equal(t, user.Id, 9438500)

	user, e = getNga().GetUserById(9438500)
	assert.Equal(t, e, nil)
	assert.Equal(t, user.Id, 9438500)

	_, e = getNga().GetUser("apt10086")
	assert.NotEqual(t, e, nil)
	assert.NotEqual(t, e.Error(), nil)

	user, e = getNga().GetUser("菜鸟牧师宫商角徵羽")
	assert.Equal(t, e, nil)
	assert.Equal(t, user.Id, 8905070)
}

func TestGetUserPost(t *testing.T) {
	post, e := getNga().GetUserPost(9438500, 0)
	assert.Equal(t, e, nil)
	assert.NotEqual(t, post, nil)
	println(post)
}

func TestModUser(t *testing.T) {
	users := make(map[string]mgr.User)
	users["aot10086"] = mgr.User{Id: 9438500, Name: "aot10086"}
	if user, ok := users["aot10086"]; ok {
		user.Subscribed = true
	}
	assert.Equal(t, users["aot10086"].Subscribed, false)
	if user, ok := users["aot10086"]; ok {
		user.Subscribed = true
		users["aot10086"] = user
	}
	assert.Equal(t, users["9e6"].Subscribed, true)
}
