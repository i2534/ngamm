package mgr

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"gopkg.in/ini.v1"
)

var (
	BaiduPCSName = "BaiduPCS-Go"
	BdpcsUserINI = "user.ini"
	BaiduBaseDir = "/我的资源"
)

type transferTask struct {
	topicId  int
	url      string
	unzipPwd string
}

type Baidu struct {
	cfg     BaiduPCS
	root    string
	program string
	mutex   *sync.Mutex
	tasks   chan transferTask
}

func NewBaidu(cfg BaiduPCS) *Baidu {
	return &Baidu{
		cfg:   cfg,
		mutex: &sync.Mutex{},
	}
}

func (b Baidu) Name() string {
	return "baidu"
}

func (b Baidu) Support(pmd PanMetadata) bool {
	if pmd.URL == "" {
		return false
	}
	return strings.Contains(pmd.URL, "pan.baidu.com")
}

func (b *Baidu) initPath() error {
	wd, e := os.Getwd()
	if e != nil {
		return fmt.Errorf("获取当前工作目录出现问题: %s", e.Error())
	}
	root := b.cfg.Root
	if root == "" {
		root = "baidupcs"
	}
	root = JoinPath(wd, root)
	b.root = root

	program := filepath.Join(root, BaiduPCSName)
	if !IsExist(program) {
		return fmt.Errorf("%s 程序不存在", program)
	}
	b.program = program
	return nil
}

func (b *Baidu) Init() error {
	b.mutex.Lock()

	if b.root == "" || b.program == "" {
		if e := b.initPath(); e != nil {
			b.mutex.Unlock()
			return e
		}
	}

	taskInit := b.tasks == nil
	b.mutex.Unlock()

	if e := b.update(); e != nil {
		return e
	}

	if v, e := b.version(); e != nil {
		return e
	} else {
		log.Printf("BaiduPCS: 版本号: %s\n", v)
	}

	if taskInit {
		b.mutex.Lock()
		if b.tasks == nil {
			b.tasks = make(chan transferTask, 100)
			go func() {
				log.Println("BaiduPCS: 启动任务处理协程")
				for task := range b.tasks {
					if e := b.doTransfer(task); e != nil {
						log.Println(e.Error())
					}
				}
			}()
		}
		b.mutex.Unlock()
	}

	return b.login()
}

func (b Baidu) optResult(out string) (int, string) {
	re := regexp.MustCompile(`代码: (\d+), 消息: (.+)`)
	match := re.FindStringSubmatch(out)
	if len(match) > 1 {
		code, msg := match[1], match[2]
		if code != "" && code != "0" {
			v, _ := strconv.Atoi(code)
			return v, msg
		}
	}
	return 0, ""
}

func (b *Baidu) doTransfer(task transferTask) error {
	log.Printf("BaiduPCS: 处理转存任务: %d, %s\n", task.topicId, task.url)
	dir := fmt.Sprintf("%s/%d", BaiduBaseDir, task.topicId)
	if v, e := b.execute("mkdir", dir); e != nil {
		return fmt.Errorf("BaiduPCS: mkdir %s 出现问题: %s", dir, e.Error())
	} else {
		code, msg := b.optResult(v)
		if code != 0 {
			log.Printf("BaiduPCS: mkdir %s 失败, 代码: %d, 消息: %s\n", dir, code, msg)
		}
	}
	if v, e := b.execute("cd", dir); e != nil {
		return fmt.Errorf("BaiduPCS: cd %s 出现问题: %s", dir, e.Error())
	} else {
		code, msg := b.optResult(v)
		if code != 0 {
			log.Printf("BaiduPCS: cd %s 失败, 代码: %d, 消息: %s\n", dir, code, msg)
		} else {
			log.Printf("BaiduPCS: cd 成功, %s\n", v)
		}
	}
	if v, e := b.execute("pwd"); e != nil {
		return fmt.Errorf("BaiduPCS: pwd 出现问题: %s", e.Error())
	} else {
		code, msg := b.optResult(v)
		if code != 0 {
			log.Printf("BaiduPCS: pwd 失败, 代码: %d, 消息: %s\n", code, msg)
		} else {
			log.Printf("BaiduPCS: pwd 成功, %s\n", v)
			if v != dir {
				return fmt.Errorf("BaiduPCS: cd 失败, 目录不正确, %s != %s", v, dir)
			}
		}
	}
	uap := task.url
	if v, e := b.execute("transfer", uap); e != nil {
		return fmt.Errorf("BaiduPCS: transfer %s 出现问题: %s", uap, e.Error())
	} else {
		if strings.Contains(v, "成功") {
			log.Printf("BaiduPCS: transfer %s 成功, %s\n", uap, v)
		} else {
			log.Printf("BaiduPCS: transfer %s 失败, %s\n", uap, v)
		}
	}

	pwd := task.unzipPwd
	if pwd != "" {
		f := filepath.Join(os.TempDir(), "_uzp.txt")
		defer os.Remove(f)

		if e := os.WriteFile(f, []byte(pwd), 0644); e != nil {
			log.Printf("BaiduPCS: 写入解压密码文件 %s 出现问题: %s", f, e.Error())
		} else {
			b.upload(f, dir)
		}
	}

	return nil
}

func (b *Baidu) Transfer(topicId int, md PanMetadata) error {
	uap := md.URL
	if !strings.Contains(md.URL, "?pwd=") {
		if md.Tqm == "" {
			return fmt.Errorf("BaiduPCS: 请输入提取码")
		}
		uap += "?pwd=" + md.Tqm
	}

	b.tasks <- transferTask{
		topicId:  topicId,
		url:      uap,
		unzipPwd: md.Pwd,
	}

	return nil
}

func (b *Baidu) login() error {
	needLogin := true
	if who, e := b.execute("who"); e != nil {
		return fmt.Errorf("BaiduPCS: who 出现问题: %s", e.Error())
	} else {
		re := regexp.MustCompile(`uid: (\d+)`)
		match := re.FindStringSubmatch(who)
		if len(match) > 1 {
			uid := match[1]
			if uid != "" && uid != "0" {
				log.Printf("BaiduPCS: login, uid: %s\n", uid)
				needLogin = false
			}
		}
	}
	if needLogin {
		bduss := b.cfg.Bduss
		stoken := b.cfg.Stoken

		fpu := filepath.Join(b.root, BdpcsUserINI)
		if IsExist(fpu) {
			user, e := ini.Load(filepath.Join(b.root, BdpcsUserINI))
			if e != nil {
				return fmt.Errorf("BaiduPCS: 读取配置文件 %s 出现问题: %s", BdpcsUserINI, e.Error())
			}
			bduss = user.Section("").Key("bduss").String()
			stoken = user.Section("").Key("stoken").String()
		}

		if bduss == "" || stoken == "" {
			return fmt.Errorf("BaiduPCS: 请设置 bduss 和 stoken")
		}
		if v, e := b.execute("login",
			fmt.Sprintf("-bduss=%s", bduss),
			fmt.Sprintf("-stoken=%s", stoken)); e != nil {
			return fmt.Errorf("BaiduPCS: login 出现问题: %s", e.Error())
		} else {
			log.Printf("BaiduPCS: %s\n", v)
			if !strings.Contains(v, "登录成功") {
				return fmt.Errorf("BaiduPCS: login 失败: %s", v)
			}
		}
	}
	return nil
}

func (b *Baidu) upload(file, dir string) error {
	if !IsExist(file) {
		return fmt.Errorf("BaiduPCS: 文件 %s 不存在", file)
	}
	if v, e := b.execute("upload", file, dir); e != nil {
		return fmt.Errorf("BaiduPCS: upload %s 出现问题: %s", file, e.Error())
	} else {
		log.Printf("BaiduPCS: upload %s 输出: %s\n", file, v)
	}
	return nil
}

func (b *Baidu) version() (string, error) {
	out, e := b.execute("-v")
	if e != nil {
		return "", fmt.Errorf("BaiduPCS: version 出现问题: %s", e.Error())
	}
	return out, nil
}

func (b *Baidu) update() error {
	out, e := b.execute("update", "-y")
	if e != nil {
		return fmt.Errorf("BaiduPCS: update 出现问题: %s", e.Error())
	}
	log.Printf("BaiduPCS: update 输出: %s\n", out)
	return nil
}

func (b *Baidu) execute(args ...string) (string, error) {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	cmd := exec.Command(b.program, args...)
	cmd.Dir = b.root
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, fmt.Sprintf("BAIDUPCS_GO_CONFIG_DIR=%s", b.root))
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if e := cmd.Run(); e != nil {
		if e, ok := e.(*exec.ExitError); ok {
			log.Printf("BaiduPCS: 执行返回非零退出状态: %s\n", e)
			return out.String(), nil
		}
		return out.String(), e
	}
	return strings.TrimSpace(out.String()), nil
}

func (b *Baidu) Close() error {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	if b.tasks != nil {
		close(b.tasks)
		b.tasks = nil
	}
	return nil
}
