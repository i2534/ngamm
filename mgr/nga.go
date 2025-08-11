package mgr

import (
	"bytes"
	_ "embed"
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
	"github.com/imroc/req/v3"
	"github.com/robfig/cron/v3"
	"gopkg.in/ini.v1"
)

const (
	NGA_CFG         = "config.ini"
	ATTACHMENT_CFG  = "attachment.ini"
	USER_DIR        = "users"
	ATTACHMENT_BASE = "https://img.nga.178.com/attachments/"
)

var (
	groupNGA = log.GROUP_NGA
)

type User struct {
	Id         int          `json:"id"`
	SubFilter  *[]string    `json:"filter,omitempty"`
	subCronId  cron.EntryID // 订阅任务的 ID
	forSubTask *time.Timer  // 启动后准备开始订阅任务的定时器
	Name       string       `json:"name"`
	Loc        string       `json:"loc"`
	RegDate    CustomTime   `json:"regDate"`
	Subscribed bool         `json:"subscribed"`
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

type fixRecord struct {
	topic *Topic
	fixes map[string]string // 需要修复的资产文件名和对应的内容
}

type Client struct {
	root      *ExtRoot
	dir       string // root 的绝对路径
	program   string // ngapost2md 程序路径
	topics    string // 帖子目录
	ua        string
	baseURL   string
	uid       string
	cid       string
	users     *users
	cron      *cron.Cron
	srv       *Server
	lock      *sync.Mutex // program exec lock
	fixCh     chan fixRecord
	attachCfg *AttachConfig // 附件下载配置
	useNetPic bool
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
		fixCh:   make(chan fixRecord, 999999),
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

	updateConfig(cfg, fp)

	client.useNetPic = cfg.Section("post").Key("use_network_pic_url").MustBool(false)

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

	client.attachCfg, e = LoadAttachmentConfig(filepath.Join(dir, ATTACHMENT_CFG))
	if e != nil {
		log.Printf("加载附件配置文件失败: %s\n", e.Error())
	}

	client.cron.Start()

	go client.doFixAsset()

	return client, nil
}

type AttachConfig struct {
	Base      AttachBaseConfig  `ini:"base"`
	UserAgent AttachUserAgent   `ini:"ua"`
	Header    map[string]string `ini:"header"`
	Proxy     AttachProxyConfig `ini:"proxy"`
	predined  map[string][]string
}

type AttachBaseConfig struct {
	Timeout     time.Duration `ini:"timeout"`
	MinDelay    time.Duration `ini:"min_delay"`
	MaxDelay    time.Duration `ini:"max_delay"`
	AutoDown    bool          `ini:"auto_down"`
	AutoReplace bool          `ini:"auto_replace"`
}

//go:embed user_agents.json
var userAgents []byte

type AttachUserAgent struct {
	Type  string `ini:"type"`
	Value string `ini:"value"`
}

type AttachProxyConfig struct {
	URL string `ini:"url"`
}

// LoadAttachmentConfig 加载配置文件
func LoadAttachmentConfig(filepath string) (*AttachConfig, error) {
	config := &AttachConfig{
		Header:   make(map[string]string),
		predined: make(map[string][]string),
	}

	cfg, e := ini.Load(filepath)
	if e != nil {
		log.Printf("加载附件配置文件失败: %s\n", e.Error())
	} else {
		// 映射 base 和 proxy 部分
		if e := cfg.MapTo(config); e != nil {
			log.Printf("映射附件配置失败: %s\n", e.Error())
		} else {
			// 手动处理 header 部分，因为它是动态的
			hs := cfg.Section("header")
			for _, key := range hs.Keys() {
				// 移除反引号包围的值
				value := key.Value()
				if len(value) >= 2 && value[0] == '`' && value[len(value)-1] == '`' {
					value = value[1 : len(value)-1]
				}
				config.Header[key.Name()] = value
			}
		}
	}

	if IsZero(config.Base) {
		config.Base = AttachBaseConfig{
			Timeout:     10 * time.Second, // 默认超时时间
			MinDelay:    1 * time.Second,  // 默认最小延迟
			MaxDelay:    5 * time.Second,  // 默认最大延迟
			AutoDown:    true,             // 默认启用自动下载
			AutoReplace: true,             // 默认启用自动替换
		}
	}
	if IsZero(config.UserAgent) {
		config.UserAgent = AttachUserAgent{
			Type: "Random",
		}
	}

	if len(userAgents) > 0 {
		var predefinedAgents map[string][]string
		if e := json.Unmarshal(userAgents, &predefinedAgents); e != nil {
			log.Printf("解析预定义用户代理失败: %s\n", e.Error())
		}
		config.predined = predefinedAgents
	}

	return config, nil
}

func InitReqClient() *req.Client {
	// 初始化 req 客户端
	client := req.C().
		SetTimeout(10*time.Second).
		SetCommonRetryCount(3).
		SetCommonRetryBackoffInterval(1*time.Second, 3*time.Second).
		DisableAutoDecode()
	return client
}

func updateConfig(cfg *ini.File, path string) {
	changed := false

	secCfg := cfg.Section("config")
	if secCfg.Key("version").String() != "1.8.0" {
		log.Println("更新配置文件到版本 1.8.0")
		secCfg.Key("version").SetValue("1.8.0")

		changed = true
	}

	secPost := cfg.Section("post")
	if secPost.Key("use_network_pic_url").String() == "" {
		log.Println("添加 post.use_network_pic_url 配置项")
		key, e := secPost.NewKey("use_network_pic_url", "True")
		if e != nil {
			log.Println("添加 post.use_network_pic_url 配置项失败:", e.Error())
		} else {
			key.Comment = "[#109]是否直接使用图片的在线链接，而不是将图片资源下载到本地后做本地图片引用。默认值False（不启用）。"
			changed = true
		}
	} else if secPost.Key("use_network_pic_url").String() == "False" {
		log.Println("修改 post.use_network_pic_url 配置项为 True")
		secPost.Key("use_network_pic_url").SetValue("True")
		changed = true
	}

	if changed {
		cfg.SaveTo(path)
	}
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
func (c Client) IsUseNetworkPic() bool {
	return c.useNetPic
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
		if after, ok := strings.CutPrefix(line, "ngapost2md"); ok {
			return strings.TrimSpace(after), nil
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
	log.Group(groupNGA).Printf("请求 %s\n", url)

	resp, e := InitReqClient().
		SetUserAgent(c.GetUA()).
		R().
		SetHeader("Cookie", "ngaPassportUid="+c.uid+"; ngaPassportCid="+c.cid).
		Get(url)

	if e != nil {
		return "", fmt.Errorf("请求 %s 失败: %w", url, e)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("请求 %s 失败, 状态码 %d", url, resp.StatusCode)
	}

	// req/v3 会自动处理响应体的读取和关闭
	data, e := GBKReadAll(strings.NewReader(resp.String()))
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

	close(c.fixCh)
	for range c.fixCh {
		// 清空 fixCh 中的所有记录
	}
	return nil
}

func (c *Client) AddFixAsset(topic *Topic, fixes map[string]string) {
	if topic == nil || len(fixes) == 0 {
		return
	}
	if topic.IsClosed() {
		return
	}
	c.fixCh <- fixRecord{
		topic: topic,
		fixes: fixes,
	}
}

func (c *Client) doFixAsset() {
	for record := range c.fixCh {
		if record.topic.IsClosed() {
			continue
		}
		c.processFixRecord(record)
		// 随机等待，避免处理过快和固定模式
		minDelay := c.attachCfg.Base.MinDelay
		maxDelay := c.attachCfg.Base.MaxDelay
		if minDelay <= 0 {
			minDelay = 1 * time.Second // 默认最小延迟 1 秒
		}
		if maxDelay <= 0 {
			maxDelay = 5 * time.Second // 默认最大延迟 5 秒
		}
		time.Sleep(minDelay + time.Duration(rand.Int63n(int64(maxDelay-minDelay))))
	}
}
func (c Client) joinAttachmentPath(name string) string {
	if strings.HasPrefix(name, ATTACHMENT_BASE) {
		return name
	}
	if after, ok := strings.CutPrefix(name, "/"); ok {
		name = after
	}
	return ATTACHMENT_BASE + "/" + name
}

func (c *Client) GetAttachment(url string) (*ResponseReader, error) {
	log.Printf("获取附件: %s\n", url)

	// 创建请求并设置超时和头部信息
	rc := InitReqClient().SetTimeout(c.attachCfg.Base.Timeout)

	// 设置 User-Agent
	uat := c.attachCfg.UserAgent.Type
	uav := c.attachCfg.UserAgent.Value
	if uat == "Random" {
		keys := make([]string, 0, len(c.attachCfg.predined))
		for k := range c.attachCfg.predined {
			keys = append(keys, k)
		}
		if len(keys) > 0 {
			// 随机选择一个预定义的 User-Agent
			randKey := keys[rand.Intn(len(keys))]
			if agents, ok := c.attachCfg.predined[randKey]; ok {
				uat = randKey
				uav = agents[rand.Intn(len(agents))]
			}
		}
	}
	switch uat {
	case "Random":
	case "Chrome":
		if uav == "" {
			uav = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/134.0.0.0 Safari/537.3"
		}
		rc = rc.SetUserAgent(uav).SetTLSFingerprintChrome()
	case "Firefox":
		if uav == "" {
			uav = "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:141.0) Gecko/20100101 Firefox/141.0"
		}
		rc = rc.SetUserAgent(uav).SetTLSFingerprintFirefox()
	case "Edge":
		if uav == "" {
			uav = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0.0.0 Safari/537.36 Edg/138.0.0.0"
		}
		rc = rc.SetUserAgent(uav).SetTLSFingerprintEdge()
	}

	// 如果配置了代理，设置代理
	if c.attachCfg != nil && c.attachCfg.Proxy.URL != "" {
		rc = rc.SetProxyURL(c.attachCfg.Proxy.URL)
	}

	r := rc.R()
	// 设置配置文件中的自定义header
	if len(c.attachCfg.Header) > 0 {
		r = r.SetHeaders(c.attachCfg.Header)
	}

	resp, e := r.Get(url)

	if e != nil {
		log.Group(groupNGA).Printf("请求 %s 失败: %s\n", url, e.Error())
		return nil, e
	}

	if resp.StatusCode != http.StatusOK {
		log.Group(groupNGA).Printf("获取失败，状态码: %d, 响应内容: %s", resp.StatusCode, resp.String())
		return nil, fmt.Errorf("获取失败，原因: %s", resp.Status)
	}

	reader := &ResponseReader{
		ReadCloser: resp.Body,
		Header:     resp.Header,
	}

	return reader, nil
}

func (c *Client) processFixRecord(record fixRecord) {
	for name, src := range record.fixes {
		if name == "" || src == "" {
			continue
		}

		url := c.joinAttachmentPath(src)
		reader, e := c.GetAttachment(url)
		if e != nil {
			log.Printf("获取响应体失败: %s\n", e.Error())
			continue
		}

		// 立即读取完整响应体到内存，避免写入时超时
		data, e := io.ReadAll(reader)
		reader.Close()

		if e != nil {
			log.Printf("读取响应体 %s 失败: %s\n", url, e.Error())
			continue
		}

		if !IsVaildImage(data) {
			log.Printf("文件 %s 不是有效的图片, 跳过修复\n", name)
			continue
		}

		e = record.topic.root.WriteAll(name, data)
		if e != nil {
			log.Printf("写入文件 %s 失败: %s\n", name, e.Error())
			continue
		}
		log.Printf("修复帖子 %d 的资源文件 %s 成功\n", record.topic.Id, name)
	}
}
