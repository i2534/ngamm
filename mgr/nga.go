package mgr

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
	"gopkg.in/ini.v1"
)

const (
	NGA_CFG   = "config.ini"
	USER_JSON = "users.json"
)

type User struct {
	Id              int        `json:"id"`
	Name            string     `json:"name"`
	Loc             string     `json:"loc"`
	RegDate         CustomTime `json:"regDate"`
	Subscribed      bool       `json:"subscribed"`
	subscribeCronId cron.EntryID
	bootSubTask     *time.Timer
}

type byUid []User

func (a byUid) Len() int           { return len(a) }
func (a byUid) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a byUid) Less(i, j int) bool { return a[i].Id < a[j].Id }

type users struct {
	lock     *sync.RWMutex
	data     map[string]User
	file     string
	saveTask *time.Timer
}

func (u *users) load() {
	u.lock.Lock()
	defer u.lock.Unlock()

	us := make([]User, 0)
	fp := u.file
	if IsExist(fp) {
		data, e := os.ReadFile(fp)
		if e != nil {
			log.Printf("读取用户信息文件失败: %s\n", e.Error())
		} else {
			e = json.Unmarshal(data, &us)
			if e != nil {
				log.Printf("解析用户信息失败: %s\n", e.Error())
			}
		}
	}
	u.data = make(map[string]User)
	for _, user := range us {
		u.data[user.Name] = user
	}
}

func (u *users) save() {
	u.lock.Lock()
	defer u.lock.Unlock()

	us := make([]User, 0, len(u.data))
	for _, user := range u.data {
		us = append(us, user)
	}

	sort.Sort(byUid(us))

	data, e := json.Marshal(us)
	if e != nil {
		log.Printf("保存用户信息失败: %s\n", e.Error())
		return
	}

	fp := u.file
	if e := os.WriteFile(fp, data, 0644); e != nil {
		log.Printf("保存用户信息失败: %s\n", e.Error())
	}
}

func (u *users) Get(name string) (User, bool) {
	u.lock.RLock()
	defer u.lock.RUnlock()

	user, ok := u.data[name]
	return user, ok
}
func (u *users) GetByUid(uid int) (User, bool) {
	u.lock.RLock()
	defer u.lock.RUnlock()

	for _, user := range u.data {
		if user.Id == uid {
			return user, true
		}
	}
	return User{}, false
}
func (u *users) Put(name string, user User) {
	u.lock.Lock()
	defer u.lock.Unlock()

	u.data[name] = user

	if u.saveTask != nil {
		u.saveTask.Stop()
	}
	u.saveTask = time.AfterFunc(30*time.Second, func() {
		u.save()
	})
}

func (u *users) Close() error {
	u.lock.Lock()
	defer u.lock.Unlock()
	if u.saveTask != nil {
		u.saveTask.Stop()
	}
	for _, user := range u.data {
		if user.bootSubTask != nil {
			user.bootSubTask.Stop()
		}
	}
	return nil
}

type Client struct {
	program string
	version string
	root    string
	cfg     *ini.File
	ua      string
	baseURL string
	uid     string
	cid     string
	users   *users
	cron    *cron.Cron
	srv     *Server
}

func InitNGA(program string) (*Client, error) {
	dir := filepath.Dir(program)
	client := &Client{
		program: program,
		root:    dir,
		users: &users{
			lock: &sync.RWMutex{},
			data: make(map[string]User),
			file: filepath.Join(dir, USER_JSON),
		},
		cron: cron.New(cron.WithLocation(TIME_LOC)),
	}
	version := client.GetVersion()
	if version == "" {
		return nil, errors.New("无法获取 ngapost2md 版本")
	}
	log.Printf("ngapost2md 版本: %s\n", version)
	fp := filepath.Join(dir, NGA_CFG)
	if _, err := os.Stat(fp); os.IsNotExist(err) {
		client.execute([]string{"--gen-config-file"})
	}
	cfg, err := ini.Load(fp)
	if err != nil {
		return nil, err
	}
	network := cfg.Section("network")
	ua := network.Key("ua").String()
	if isEnclosed(ua, '<', '>') {
		return nil, errors.New("请在配置文件中填写正确的 network.ua")
	}
	uid := network.Key("ngaPassportUid").String()
	if isEnclosed(uid, '<', '>') {
		return nil, errors.New("请在配置文件中填写正确的 network.ngaPassportUid")
	}
	cid := network.Key("ngaPassportCid").String()
	if isEnclosed(cid, '<', '>') {
		return nil, errors.New("请在配置文件中填写正确的 network.ngaPassportCid")
	}

	client.version = version
	client.cfg = cfg
	client.ua = ua
	client.baseURL = network.Key("base_url").String()
	client.uid = uid
	client.cid = cid
	client.users.load()

	for un, user := range client.users.data {
		delay := time.Duration(rand.Intn(600)) * time.Second // 10 分钟内随机, 避免同时发送请求
		user.bootSubTask = time.AfterFunc(delay, func() {
			nu := &user
			if e := client.doSubscribe(nu); e != nil {
				log.Printf("订阅用户 %s 出现问题: %s\n", nu.Name, e.Error())
			}
		})
		client.users.data[un] = user
	}

	client.cron.Start()

	return client, nil
}

func isEnclosed(s string, start, end rune) bool {
	if len(s) < 2 {
		return false
	}
	return rune(s[0]) == start && rune(s[len(s)-1]) == end
}

func (c *Client) GetRoot() string {
	return c.root
}

func (c *Client) GetVersion() string {
	if c.version == "" {
		out, e := c.execute([]string{"-v"})
		if e != nil {
			return ""
		}
		lines := strings.Split(out, "\n")
		if len(lines) > 0 {
			line := lines[0]
			if strings.HasPrefix(line, "ngapost2md") {
				c.version = strings.TrimSpace(strings.TrimPrefix(line, "ngapost2md"))
			}
		}
	}
	return c.version
}

func (c *Client) GetUA() string {
	return c.ua
}
func (c *Client) BaseURL() string {
	return c.baseURL
}

func (c *Client) DownTopic(tid int) (bool, string) {
	out, err := c.execute([]string{strconv.Itoa(tid)})
	if err != nil {
		log.Printf("下载主题 %d 出现问题: %s\n", tid, err.Error())
	} else {

		log.Printf("\n%s", out)

		lines := strings.Split(out, "\n")
		for i := len(lines) - 1; i >= 0; i-- {
			line := lines[i]
			if strings.TrimSpace(line) == "" {
				continue
			}
			if strings.Contains(line, "任务结束") {
				log.Printf("下载主题 %d 完成\n", tid)
				return true, ""
			}
			i := strings.Index(line, "返回代码不为")
			if i > 0 {
				msg := line[i:]
				log.Printf("下载主题 %d 出现问题: %s\n", tid, msg)
				return false, msg
			}
		}
	}
	return false, ""
}

func (c *Client) getHTML(url string) (string, error) {
	req, e := http.NewRequest(http.MethodGet, url, nil)
	if e != nil {
		return "", fmt.Errorf("failed to create request: %w", e)
	}
	req.Header.Set("User-Agent", c.GetUA())
	req.Header.Set("Cookie", "ngaPassportUid="+c.uid+"; ngaPassportCid="+c.cid)

	resp, e := DoHttp(req)
	if e != nil {
		return "", fmt.Errorf("failed to get %s: %w", url, e)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to get %s: status code %d", url, resp.StatusCode)
	}

	data, e := io.ReadAll(resp.Body)
	if e != nil {
		return "", fmt.Errorf("failed to read response: %w", e)
	}

	reader := transform.NewReader(bytes.NewReader(data), simplifiedchinese.GBK.NewDecoder())
	decodedData, e := io.ReadAll(reader)
	if e != nil {
		return "", fmt.Errorf("failed to decode response: %w", e)
	}
	return string(decodedData), nil
}

func (c *Client) GetUser(username string) (User, error) {
	if u, ok := c.users.Get(username); ok {
		return u, nil
	}
	escaped, e := PathEscapeGBK(username)
	if e != nil {
		return User{}, e
	}
	url := fmt.Sprintf("%s/nuke.php?func=ucp&username=%s", c.baseURL, escaped)
	html, e := c.getHTML(url)
	if e != nil {
		return User{}, e
	}

	// println(html)

	if strings.Contains(html, "找不到用户") {
		return User{}, fmt.Errorf("user not found")
	}

	re := regexp.MustCompile(`__UCPUSER\s*=(.+)?;`)
	matches := re.FindStringSubmatch(html)
	if len(matches) < 2 {
		return User{}, fmt.Errorf("failed to find user info")
	}
	val := strings.TrimSpace(matches[1])
	if len(val) > 0 {
		info := make(map[string]any)
		e := json.Unmarshal([]byte(val), &info)
		if e != nil {
			return User{}, fmt.Errorf("failed to parse user info: %s, cause by: %w", val, e)
		}
		uid := int(info["uid"].(float64))
		ipLoc := info["ipLoc"].(string)
		regDate := int64(info["regdate"].(float64))

		log.Printf("获取到用户 %s 的 UID: %d\n", username, uid)

		u := User{
			Id:      uid,
			Name:    username,
			Loc:     ipLoc,
			RegDate: CustomTime{time.Unix(regDate, 0)},
		}
		c.users.Put(username, u)
		return u, nil
	}
	return User{}, fmt.Errorf("get empty UID")
}

type topicRecord struct {
	Id    int
	Title string
	Miss  bool
}

func (c *Client) GetUserPost(uid, from int) ([]topicRecord, error) {
	// https://ngabbs.com/thread.php?authorid=166963&fid=0&page=1
	url := fmt.Sprintf("%s/thread.php?authorid=%d", c.baseURL, uid) // 现在只取第一页的帖子
	html, e := c.getHTML(url)
	if e != nil {
		return nil, e
	}

	// println(html)

	re := regexp.MustCompile(`<a href='/read.php\?tid=(\d+)' id='(.*?)' class='topic'>(.*?)</a>`)
	matches := re.FindAllStringSubmatch(html, -1)
	if len(matches) == 0 {
		return nil, fmt.Errorf("failed to find user post")
	}
	posts := make([]topicRecord, 0)
	for _, match := range matches {
		tid, e := strconv.Atoi(match[1])
		if e != nil {
			continue
		}
		if tid <= from {
			continue
		}
		miss := false
		span := strings.TrimSpace(match[3])
		if strings.HasPrefix(span, "<span") {
			miss = true
			span = span[strings.Index(span, ">")+1:]
		}
		title := strings.TrimSuffix(span, "</span>")
		posts = append(posts, topicRecord{
			Id:    tid,
			Title: title,
			Miss:  miss,
		})
	}
	return posts, nil
}

func (c *Client) doSubscribe(user *User) error {
	if user != nil && user.Subscribed {
		if user.subscribeCronId > 0 {
			c.cron.Remove(user.subscribeCronId)
		}
		if id, e := c.cron.AddFunc("@every 3m", func() {
			topics := c.srv.getTopics(user.Name)
			max := 0
			for _, topic := range topics {
				if topic.Id > max {
					max = topic.Id
				}
			}

			newest, e := c.GetUserPost(user.Id, max)
			if e != nil {
				log.Printf("获取用户 %s(%d) 的帖子失败: %s\n", user.Name, user.Id, e.Error())
				return
			}

			log.Printf("获取用户 %s(%d) 新的帖子数量: %d\n", user.Name, user.Id, len(newest))

			for _, topic := range newest {
				if topic.Miss {
					log.Printf("用户 %s(%d) 的主题 %d 已无法访问\n", user.Name, user.Id, topic.Id)
					continue
				}
				e = c.srv.addTopic(topic.Id)
				if e != nil {
					log.Printf("添加帖子 %d 失败: %s\n", topic.Id, e.Error())
				}
			}
		}); e != nil {
			return e
		} else {
			user.subscribeCronId = id
		}
	}
	return nil
}
func (c *Client) Subscribe(uid int, status bool) error {
	if u, ok := c.users.GetByUid(uid); ok {
		log.Printf("变更用户 %s(%d) 订阅状态: %v\n", u.Name, u.Id, status)
		if status {
			if !u.Subscribed {
				nu := &u
				nu.Subscribed = true
				if e := c.doSubscribe(nu); e != nil {
					return e
				}
				c.users.Put(nu.Name, *nu)
			}
		} else {
			if u.Subscribed {
				if u.subscribeCronId > 0 {
					c.cron.Remove(u.subscribeCronId)
				}
				if u.bootSubTask != nil {
					u.bootSubTask.Stop()
					u.bootSubTask = nil
				}
				u.Subscribed = false
				c.users.Put(u.Name, u)
			}
		}
		return nil
	}
	return fmt.Errorf("user not found")
}

func (c *Client) execute(args []string) (string, error) {
	cmd := exec.Command(c.program, args...)
	cmd.Dir = c.root
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	if err != nil {
		if e, ok := err.(*exec.ExitError); ok {
			log.Printf("命令执行返回非零退出状态: %s\n", e)
			return out.String(), nil
		}
		return out.String(), err
	}
	return out.String(), nil
}

func (c *Client) Close() error {
	c.users.save()
	c.users.Close()
	c.cron.Stop()
	return nil
}
