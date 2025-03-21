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
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
	"gopkg.in/ini.v1"
)

const (
	NGA_CFG  = "config.ini"
	USER_DIR = "users"
)

type User struct {
	Id              int          `json:"id"`
	Name            string       `json:"name"`
	Loc             string       `json:"loc"`
	RegDate         CustomTime   `json:"regDate"`
	Subscribed      bool         `json:"subscribed"`
	SubFilter       *[]string    `json:"filter,omitempty"`
	subscribeCronId cron.EntryID // 订阅任务的 ID
	bootSubTask     *time.Timer  // 启动后准备订阅任务的定时器
}

type users struct {
	root   *ExtRoot
	lock   *sync.RWMutex
	data   map[string]User
	failed map[string]string //加载失败的用户
}
type userState int

const (
	state_have userState = iota + 1
	state_miss
	state_fail
)

func (u *users) load() {
	u.lock.Lock()
	defer u.lock.Unlock()

	us := make([]User, 0)
	es, e := u.root.ReadDir(".")
	if e != nil {
		log.Printf("读取用户信息目录失败: %s\n", e.Error())
		return
	}

	for _, e := range es {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		f, e := u.root.Open(e.Name())
		if e != nil {
			log.Printf("读取用户信息文件失败: %s\n", e.Error())
		} else {
			defer f.Close()
			data, e := io.ReadAll(f)
			if e != nil {
				log.Printf("读取用户信息失败: %s\n", e.Error())
			}
			var u User
			e = json.Unmarshal(data, &u)
			if e != nil {
				log.Printf("解析用户信息失败: %s\n", e.Error())
			} else {
				us = append(us, u)
			}
		}
	}

	u.data = make(map[string]User)
	for _, user := range us {
		u.data[user.Name] = user
	}
}
func (u *users) Get(name string) (User, userState) {
	u.lock.RLock()
	defer u.lock.RUnlock()

	if _, has := u.failed[name]; has {
		return User{}, state_fail
	}

	user, ok := u.data[name]
	if ok {
		return user, state_have
	}
	return user, state_miss
}
func (u *users) GetByUid(uid int) (User, userState) {
	u.lock.RLock()
	defer u.lock.RUnlock()

	if _, has := u.failed[fmt.Sprintf("UID%d", uid)]; has {
		return User{}, state_fail
	}

	for _, user := range u.data {
		if user.Id == uid {
			return user, state_have
		}
	}
	return User{}, state_miss
}
func (u *users) Put(name string, user User) {
	u.lock.Lock()
	defer u.lock.Unlock()

	u.data[name] = user

	fn := fmt.Sprintf("%d.json", user.Id)
	f, e := u.root.Create(fn)
	if e != nil {
		log.Printf("创建用户信息文件失败: %s\n", e.Error())
		return
	}
	defer f.Close()

	data, e := json.Marshal(user)
	if e != nil {
		log.Printf("序列化用户信息失败: %s\n", e.Error())
		return
	}
	if _, e := f.Write(data); e != nil {
		log.Printf("写入用户信息失败: %s\n", e.Error())
	}
}
func (u *users) Fail(name, msg string) {
	u.lock.Lock()
	defer u.lock.Unlock()

	u.failed[name] = msg
}
func (u *users) FailMsg(name string) string {
	u.lock.RLock()
	defer u.lock.RUnlock()

	return u.failed[name]
}

func (u *users) Close() error {
	u.lock.Lock()
	defer u.lock.Unlock()

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
	root    *ExtRoot
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
	er, e := OpenRoot(dir)
	if e != nil {
		return nil, e
	}
	ur, e := er.SafeOpenRoot(USER_DIR)
	if e != nil {
		return nil, e
	}

	client := &Client{
		program: program,
		root:    er,
		users: &users{
			root:   ur,
			lock:   &sync.RWMutex{},
			data:   make(map[string]User),
			failed: make(map[string]string),
		},
		cron: cron.New(cron.WithLocation(TIME_LOC)),
	}
	version := client.GetVersion()
	if version == "" {
		return nil, errors.New("无法获取 ngapost2md 版本")
	}
	log.Printf("ngapost2md 版本: %s\n", version)
	fp := filepath.Join(dir, NGA_CFG)
	if !IsExist(fp) {
		client.execute([]string{"--gen-config-file"})
	}
	cfg, e := ini.Load(fp)
	if e != nil {
		return nil, e
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

func (c *Client) GetRoot() *ExtRoot {
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
	out, e := c.execute([]string{strconv.Itoa(tid)})
	if e != nil {
		log.Printf("下载帖子 %d 出现问题: %s\n", tid, e.Error())
	} else {

		log.Printf("\n%s", out)

		lines := strings.Split(out, "\n")
		for i := len(lines) - 1; i >= 0; i-- {
			line := lines[i]
			if strings.TrimSpace(line) == "" {
				continue
			}
			if strings.Contains(line, "任务结束") {
				log.Printf("下载帖子 %d 完成\n", tid)
				return true, ""
			}
			i := strings.Index(line, "返回代码不为")
			if i > 0 {
				msg := line[i:]
				log.Printf("下载帖子 %d 出现问题: %s\n", tid, msg)
				return false, msg
			}
		}
	}
	return false, ""
}

func (c *Client) getHTML(url string) (string, error) {
	req, e := http.NewRequest(http.MethodGet, url, nil)
	if e != nil {
		return "", fmt.Errorf("创建请求失败: %w", e)
	}
	req.Header.Set("User-Agent", c.GetUA())
	req.Header.Set("Cookie", "ngaPassportUid="+c.uid+"; ngaPassportCid="+c.cid)

	log.Printf("请求 %s\n", url)

	resp, e := DoHttp(req)
	if e != nil {
		return "", fmt.Errorf("请求 %s 失败: %w", url, e)
	}
	reader, e := BodyReader(resp)
	if e != nil {
		reader.Close()
		return "", fmt.Errorf("获取响应体失败: %w", e)
	}
	defer reader.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("请求 %s 失败, 状态码 %d", url, resp.StatusCode)
	}

	data, e := GBKReadAll(reader)
	if e != nil {
		return "", fmt.Errorf("解码响应失败: %w", e)
	}
	return string(data), nil
}
func (c *Client) extractUserInfo(html string, username string) (User, error) {
	if strings.Contains(html, "找不到用户") || strings.Contains(html, "无此用户") || strings.Contains(html, "参数错误") {
		c.users.Fail(username, "找不到用户")
		return User{}, fmt.Errorf("未找到用户")
	}

	re := regexp.MustCompile(`__UCPUSER\s*=(.+)?;`)
	matches := re.FindStringSubmatch(html)
	if len(matches) < 2 {
		c.users.Fail(username, "未匹配到用户信息")
		return User{}, fmt.Errorf("未匹配到用户信息")
	}
	val := strings.TrimSpace(matches[1])
	if len(val) > 0 {
		info := make(map[string]any)
		e := json.Unmarshal([]byte(val), &info)
		if e != nil {
			c.users.Fail(username, e.Error())
			return User{}, fmt.Errorf("解析用户信息 %s 失败: %w", val, e)
		}
		uid := int(info["uid"].(float64))
		name := info["username"].(string)
		ipLoc := info["ipLoc"].(string)
		regDate := int64(info["regdate"].(float64))

		log.Printf("获取到用户 %s[%d] 的信息\n", username, uid)

		u := User{
			Id:      uid,
			Name:    name,
			Loc:     ipLoc,
			RegDate: CustomTime{time.Unix(regDate, 0)},
		}

		c.users.Put(username, u)
		if username != name {
			c.users.Put(name, u)
		}
		return u, nil
	}
	c.users.Fail(username, "匹配到空的用户信息")
	return User{}, fmt.Errorf("匹配到空的用户信息")
}
func (c *Client) GetUserById(uid int) (User, error) {
	if u, s := c.users.GetByUid(uid); s == state_have {
		return u, nil
	} else if s == state_fail {
		return User{}, fmt.Errorf("获取用户 %d 失败, 因为上次失败: %s", uid, c.users.FailMsg(fmt.Sprintf("UID%d", uid)))
	}
	url := fmt.Sprintf("%s/nuke.php?func=ucp&uid=%d", c.baseURL, uid)
	html, e := c.getHTML(url)
	if e != nil {
		return User{}, e
	}

	return c.extractUserInfo(html, fmt.Sprintf("UID%d", uid))
}
func (c *Client) GetUser(username string) (User, error) {
	if username == "" {
		return User{}, fmt.Errorf("用户名为空")
	}
	if u, s := c.users.Get(username); s == state_have {
		return u, nil
	} else if s == state_fail {
		return User{}, fmt.Errorf("获取用户 %s 失败, 因为上次失败: %s", username, c.users.FailMsg(username))
	}
	eun, e := PathEscapeGBK(username)
	if e != nil {
		return User{}, e
	}
	url := fmt.Sprintf("%s/nuke.php?func=ucp&username=%s", c.baseURL, eun)
	html, e := c.getHTML(url)
	if e != nil {
		return User{}, e
	}

	return c.extractUserInfo(html, username)
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
		return nil, fmt.Errorf("未匹配到用户帖子")
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
		if id, e := c.cron.AddFunc("@every 30m", func() {
			topics := c.srv.getTopics(user.Name)
			max := 0
			for _, topic := range topics {
				if topic.Id > max {
					max = topic.Id
				}
			}

			newest, e := c.GetUserPost(user.Id, max)
			if e != nil {
				log.Printf("获取用户 %s[%d] 的帖子失败: %s\n", user.Name, user.Id, e.Error())
				return
			}

			log.Printf("获取用户 %s[%d] 新的帖子数量: %d\n", user.Name, user.Id, len(newest))

			for _, topic := range newest {
				if topic.Miss {
					log.Printf("帖子 %d 已无法访问\n", topic.Id)
					continue
				}

				if (user.SubFilter != nil) && len(*user.SubFilter) > 0 {
					matched := false
					lowerTitle := strings.ToLower(topic.Title)
					for _, cond := range *user.SubFilter {
						cond = strings.ToLower(strings.TrimSpace(cond))
						if strings.Contains(cond, "+") { // 必须同时包含多个条件
							cs := strings.Split(cond, "+")
							cm := true
							for _, c := range cs {
								c = strings.TrimSpace(c)
								if !strings.Contains(lowerTitle, c) {
									cm = false
									break
								}
							}
							if cm {
								matched = true
								break
							}
						} else if strings.Contains(lowerTitle, cond) { // 包含任意一个条件
							matched = true
							break
						}
					}
					if !matched {
						log.Printf("帖子 %d 主题 <%s> 不匹配过滤条件\n", topic.Id, topic.Title)
						continue
					}
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
func (c *Client) Subscribe(uid int, status bool, filter ...string) error {
	if u, s := c.users.GetByUid(uid); s == state_have {
		log.Printf("变更用户 %s[%d] 订阅状态: %v\n", u.Name, u.Id, status)
		if status {
			if !u.Subscribed {
				nu := &u
				nu.Subscribed = true
				if len(filter) > 0 {
					nu.SubFilter = &filter
				}
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
	return fmt.Errorf("用户信息加载失败")
}

func (c *Client) execute(args []string) (string, error) {
	dir, e := c.root.AbsPath()
	if e != nil {
		return "", e
	}
	cmd := exec.Command(c.program, args...)
	cmd.Dir = dir
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if e := cmd.Run(); e != nil {
		if e, ok := e.(*exec.ExitError); ok {
			log.Printf("命令执行返回非零退出状态: %s\n", e)
			return out.String(), nil
		}
		return out.String(), e
	}
	return out.String(), nil
}

func (c *Client) Close() error {
	c.users.Close()
	c.cron.Stop()
	c.root.Close()
	return nil
}
