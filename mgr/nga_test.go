package mgr_test

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
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

func TestExtractUserAgents(t *testing.T) {
	req, err := http.NewRequest("GET", "https://raw.githubusercontent.com/fake-useragent/fake-useragent/refs/heads/main/src/fake_useragent/data/browsers.jsonl", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	result := make(map[string][]string)

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		data := make(map[string]any)
		if err := json.Unmarshal([]byte(line), &data); err != nil {
			t.Fatal(err)
		}
		if data["type"] == "desktop" {
			browser, ok := data["browser"].(string)
			if !ok {
				t.Fatalf("Expected browser to be a string, got %T", data["browser"])
			}

			if browser != "Chrome" && browser != "Firefox" && browser != "Edge" {
				t.Logf("Skipping browser: %s", browser)
				continue
			}

			userAgent, ok := data["useragent"].(string)
			if !ok {
				t.Fatalf("Expected useragent to be a string, got %T", data["useragent"])
			}

			if _, exists := result[browser]; !exists {
				result[browser] = []string{}
			}

			array := result[browser]

			has := false
			for _, existing := range array {
				if existing == userAgent {
					t.Logf("Skipping duplicate User-Agent: %s for browser: %s", userAgent, browser)
					has = true
					break
				}
			}
			if has {
				continue
			}

			result[browser] = append(array, userAgent)
			t.Logf("Browser: %s, User-Agent: %s", browser, userAgent)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}

	out, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	outFile := "../data/user_agents.json"
	if err := os.WriteFile(outFile, out, 0644); err != nil {
		t.Fatalf("Failed to write to file %s: %v", outFile, err)
	}
	t.Logf("User agents extracted and saved to %s", outFile)
}

func TestGetAttachment(t *testing.T) {
	url := "https://img.nga.178.com/attachments/mon_202508/01/-7Q1ag-2t34K1vT3cSu0-sr.jpg"
	r, e := getNga().GetAttachment(url)
	if e != nil {
		t.Error("Get attachment failed:", e)
		return
	}
	defer r.Close()

	buf := make([]byte, 1024)
	n, e := r.Read(buf)
	if e != nil && e != io.EOF {
		t.Error("Read attachment failed:", e)
		return
	}
	if n == 0 {
		t.Error("Read attachment returned no data")
		return
	}
	t.Logf("Attachment %s downloaded successfully", url)
	t.Logf("Read %d bytes, %s", n, buf[:n])
}
