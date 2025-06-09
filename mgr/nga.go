package mgr

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/i2534/ngamm/mgr/log"
	"github.com/robfig/cron/v3"
	"gopkg.in/ini.v1"
)

const (
	NGA_CFG  = "config.ini"
	USER_DIR = "users"
)

var (
	groupNGA = log.GROUP_NGA
)

type User struct {
	Id         int          `json:"id"`
	Name       string       `json:"name"`
	Loc        string       `json:"loc"`
	RegDate    CustomTime   `json:"regDate"`
	Subscribed bool         `json:"subscribed"`
	SubFilter  *[]string    `json:"filter,omitempty"`
	subCronId  cron.EntryID // 订阅任务的 ID
	forSubTask *time.Timer  // 启动后准备开始订阅任务的定时器
	saved      bool         // 是否已经保存到文件
}

type userState int

const (
	state_have userState = iota + 1
	state_miss
	state_fail
)

type users struct {
	root   *ExtRoot
	data   *SyncMap[string, *User]  // 用户信息
	failed *SyncMap[string, string] // 加载失败的用户
}

func newUsers(root *ExtRoot) *users {
	return &users{
		root:   root,
		data:   NewSyncMap[string, *User](),
		failed: NewSyncMap[string, string](),
	}
}

func (u *users) load() {
	fs, e := u.root.ReadDir()
	if e != nil {
		log.Printf("读取用户信息目录失败: %s\n", e.Error())
		return
	}
	for _, f := range fs {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".json") {
			continue
		}
		f, e := u.root.Open(f.Name())
		if e != nil {
			log.Printf("读取用户信息文件失败: %s\n", e.Error())
		} else {
			data, e := io.ReadAll(f)
			if e != nil {
				log.Printf("读取用户信息失败: %s\n", e.Error())
			}
			user := &User{
				saved: true,
			}
			e = json.Unmarshal(data, user)
			if e != nil {
				log.Printf("解析用户信息失败: %s\n", e.Error())
			} else {
				u.data.Put(user.Name, user)
			}

			f.Close()
		}
	}
}
func (u users) uidToName(uid int) string {
	return fmt.Sprintf("UID%d", uid)
}

func (u *users) Get(name string) (*User, userState) {
	if u.failed.Has(name) {
		return nil, state_fail
	}

	user, ok := u.data.Get(name)
	if ok {
		return user, state_have
	}
	return user, state_miss
}

func (u *users) GetByUid(uid int) (*User, userState) {
	n := u.uidToName(uid)
	if u.failed.Has(n) {
		return nil, state_fail
	}

	if user, ok := u.data.Get(n); ok {
		return user, state_have
	}

	for _, user := range u.data.Values() {
		if user.Id == uid {
			return user, state_have
		}
	}

	return nil, state_miss
}

func (u *users) PutAndSave(user *User) {
	u.data.Put(user.Name, user)
	un := u.uidToName(user.Id)
	if user.Name != un {
		u.data.Put(un, user)
	}

	if user.saved {
		return
	}

	data, e := json.Marshal(user)
	if e != nil {
		log.Printf("序列化用户信息失败: %s\n", e.Error())
		return
	}
	f, e := u.root.Create(fmt.Sprintf("%d.json", user.Id))
	if e != nil {
		log.Printf("创建用户信息文件失败: %s\n", e.Error())
		return
	}
	defer f.Close()
	if _, e := f.Write(data); e != nil {
		log.Printf("写入用户信息失败: %s\n", e.Error())
	}

	user.saved = true
}

func (u *users) Fail(name, msg string) {
	u.failed.Put(name, msg)
}
func (u *users) FailMsg(name string) string {
	if msg, ok := u.failed.Get(name); ok {
		return msg
	}
	return ""
}

func (u *users) Close() error {
	u.data.EAC((func(name string, user *User) {
		if user.forSubTask != nil {
			user.forSubTask.Stop()
		}
	}))
	u.root.Close()
	return nil
}

type Client struct {
	root    *ExtRoot
	dir     string // root 的绝对路径
	program string // ngapost2md 程序路径
	topics  string // 帖子目录
	ua      string
	baseURL string
	uid     string
	cid     string
	users   *users
	cron    *cron.Cron
	srv     *Server
	lock    *sync.Mutex // program exec lock
}

func InitNGA(global Config) (*Client, error) {
	program := global.Program
	dir := filepath.Dir(program)

	topics := dir
	if global.TopicRoot != "" {
		if !filepath.IsAbs(global.TopicRoot) {
			topics = filepath.Join(dir, global.TopicRoot)
		}
	}
	topics, e := filepath.Abs(topics)
	if e != nil {
		return nil, e
	}

	root, e := OpenRoot(dir)
	if e != nil {
		return nil, e
	}
	dir, e = root.AbsPath()
	if e != nil {
		return nil, e
	}

	userDir, e := root.SafeOpenRoot(USER_DIR)
	if e != nil {
		return nil, e
	}

	client := &Client{
		root:    root,
		dir:     dir,
		program: program,
		topics:  topics,
		users:   newUsers(userDir),
		cron:    cron.New(cron.WithLocation(TIME_LOC)),
		lock:    &sync.Mutex{},
	}

	version, e := client.version()
	if e != nil || version == "" {
		msg := ""
		if e != nil {
			msg = e.Error()
		}
		return nil, fmt.Errorf("无法获取 ngapost2md 版本 %s", msg)
	}
	log.Printf("ngapost2md 版本: %s\n", version)

	fp := filepath.Join(dir, NGA_CFG)
	if !IsExist(fp) {
		client.execute([]string{"--gen-config-file"}, dir)
	}
	cfg, e := ini.Load(fp)
	if e != nil {
		return nil, e
	}
	cfg.BlockMode = false

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

	client.ua = ua
	client.uid = uid
	client.cid = cid
	client.baseURL = network.Key("base_url").String()

	if topics != dir { // 帖子目录和程序目录不一致, 复制配置文件到帖子目录
		log.Printf("复制配置文件到帖子目录: %s\n", topics)
		if e := CopyFile(fp, filepath.Join(topics, NGA_CFG)); e != nil {
			return nil, fmt.Errorf("复制配置文件到帖子目录失败: %s", e.Error())
		}
	}

	client.users.load()
	for _, user := range client.users.data.Values() {
		delay := time.Duration(rand.Intn(600)) * time.Second // 10 分钟内随机, 避免同时发送请求
		user.forSubTask = time.AfterFunc(delay, func() {
			if e := client.doSubscribe(user); e != nil {
				log.Printf("订阅用户 %s 出现问题: %s\n", user.Name, e.Error())
			}
		})
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

func (c Client) GetRoot() *ExtRoot {
	return c.root
}
func (c Client) GetUA() string {
	return c.ua
}
func (c Client) BaseURL() string {
	return c.baseURL
}

// absolute path
func (c Client) GetTopicRoot() string {
	return c.topics
}

func (c *Client) version() (string, error) {
	out, e := c.execute([]string{"-v"}, "")
	if e != nil {
		return "", e
	}
	lines := strings.Split(out, "\n")
	if len(lines) > 0 {
		line := strings.TrimSpace(lines[0])
		if strings.HasPrefix(line, "ngapost2md") {
			return strings.TrimSpace(strings.TrimPrefix(line, "ngapost2md")), nil
		}
	}
	return "", errors.New("无输出")
}

func (c *Client) DownTopic(tid int) (bool, string) {
	out, e := c.execute([]string{strconv.Itoa(tid)}, c.topics)
	if e != nil {
		log.Printf("下载帖子 %d 出现问题: %s\n", tid, e.Error())
	} else {
		log.Group(groupNGA).Printf("\n%s", out)

		lines := strings.Split(out, "\n")
		for i := len(lines) - 1; i >= 0; i-- {
			line := strings.TrimSpace(lines[i])
			if line == "" {
				continue
			}
			if strings.Contains(line, "任务结束") {
				log.Group(groupNGA).Printf("下载帖子 %d 完成\n", tid)
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

func (c *Client) execute(args []string, dir string) (string, error) {
	c.lock.Lock()
	defer c.lock.Unlock()

	cmd := exec.Command(c.program, args...)
	if dir == "" {
		cmd.Dir = c.dir
	} else {
		cmd.Dir = dir
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if e := cmd.Run(); e != nil {
		if e, ok := e.(*exec.ExitError); ok {
			log.Group(groupNGA).Printf("命令执行返回非零退出状态: %s\n", e)
			return strings.TrimSpace(out.String()), nil
		}
		return strings.TrimSpace(out.String()), e
	}
	return strings.TrimSpace(out.String()), nil
}

func (c *Client) getHTML(url string) (string, error) {
	req, e := http.NewRequest(http.MethodGet, url, nil)
	if e != nil {
		return "", fmt.Errorf("创建请求失败: %w", e)
	}
	req.Header.Set("User-Agent", c.GetUA())
	req.Header.Set("Cookie", "ngaPassportUid="+c.uid+"; ngaPassportCid="+c.cid)

	log.Group(groupNGA).Printf("请求 %s\n", url)

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
func (c *Client) extractUserInfo(html string) (*User, error) {
	if strings.Contains(html, "找不到用户") || strings.Contains(html, "无此用户") || strings.Contains(html, "参数错误") {
		return nil, fmt.Errorf("未找到用户")
	}

	re := regexp.MustCompile(`__UCPUSER\s*=(.+)?;`)
	matches := re.FindStringSubmatch(html)
	if len(matches) < 2 {
		return nil, fmt.Errorf("未匹配到用户信息")
	}
	val := strings.TrimSpace(matches[1])
	if len(val) > 0 {
		info := make(map[string]any)
		e := json.Unmarshal([]byte(val), &info)
		if e != nil {
			return nil, fmt.Errorf("解析用户信息 %s 失败: %w", val, e)
		}
		uid := int(info["uid"].(float64))
		name := info["username"].(string)
		ipLoc := info["ipLoc"].(string)
		regDate := int64(info["regdate"].(float64))

		log.Printf("获取到用户 %s[%d] 的信息\n", name, uid)

		u := User{
			Id:      uid,
			Name:    name,
			Loc:     ipLoc,
			RegDate: CustomTime{time.Unix(regDate, 0)},
		}

		return &u, nil
	}
	return nil, fmt.Errorf("匹配到空的用户信息")
}

func (c *Client) getUserById(uid int) (*User, error) {
	url := fmt.Sprintf("%s/nuke.php?func=ucp&uid=%d", c.baseURL, uid)
	html, e := c.getHTML(url)
	if e != nil {
		return nil, e
	}
	return c.extractUserInfo(html)
}

func (c *Client) GetUserById(uid int) (User, error) {
	users := c.users
	if u, s := users.GetByUid(uid); s == state_have {
		return *u, nil
	} else if s == state_fail {
		return User{}, fmt.Errorf("获取用户 %d 失败, 因为上次失败: %s", uid, c.users.FailMsg(users.uidToName(uid)))
	}

	u, e := c.getUserById(uid)
	if e != nil {
		users.Fail(users.uidToName(uid), e.Error())
		return User{}, e
	}

	users.PutAndSave(u)
	return *u, nil
}

// 尝试从帖子中获取用户信息, 主要是更新用户名为 UIDxxx
func (c *Client) SetTopicUser(uid int, username string) {
	if username == "" {
		return
	}
	if username == c.users.uidToName(uid) {
		return
	}
	user, s := c.users.GetByUid(uid)
	if s == state_have && user.Name != username {
		user.Name = username
		user.saved = false
		c.users.PutAndSave(user)
	}
}

func (c *Client) GetUser(username string) (User, error) {
	if username == "" {
		return User{}, fmt.Errorf("用户名为空")
	}
	if u, s := c.users.Get(username); s == state_have {
		return *u, nil
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

	u, e := c.extractUserInfo(html)
	if e != nil {
		c.users.Fail(username, e.Error())
		return User{}, e
	}

	c.users.PutAndSave(u)
	return *u, nil
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
		if user.subCronId > 0 {
			c.cron.Remove(user.subCronId)
			user.subCronId = 0
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

			log.Group(groupNGA).Printf("获取用户 %s[%d] 新的帖子数量: %d\n", user.Name, user.Id, len(newest))

			for _, topic := range newest {
				if topic.Miss {
					log.Group(groupNGA).Printf("帖子 %d 已无法访问\n", topic.Id)
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
						log.Group(groupNGA).Printf("帖子 %d 主题 <%s> 不匹配过滤条件\n", topic.Id, topic.Title)
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
			user.subCronId = id
		}
	}
	return nil
}
func (c *Client) Subscribe(uid int, status bool, filter ...string) error {
	if u, s := c.users.GetByUid(uid); s == state_have {
		log.Printf("变更用户 %s[%d] 订阅状态: %v\n", u.Name, u.Id, status)
		if status {
			if !u.Subscribed {
				u.Subscribed = true
				if len(filter) > 0 {
					u.SubFilter = &filter
				}
				u.saved = false
				c.users.PutAndSave(u)

				if e := c.doSubscribe(u); e != nil {
					return e
				}
			}
		} else {
			if u.Subscribed {
				if u.subCronId > 0 {
					c.cron.Remove(u.subCronId)
					u.subCronId = 0
				}
				if u.forSubTask != nil {
					u.forSubTask.Stop()
					u.forSubTask = nil
				}
				u.Subscribed = false

				u.saved = false
				c.users.PutAndSave(u)
			}
		}
		return nil
	}
	return fmt.Errorf("用户信息加载失败")
}

func (c *Client) Close() error {
	c.users.Close()
	c.cron.Stop()
	c.root.Close()
	return nil
}
