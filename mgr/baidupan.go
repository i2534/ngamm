package mgr

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/robfig/cron/v3"
	"gopkg.in/ini.v1"
)

// use https://github.com/qjfoidnh/BaiduPCS-Go

var (
	BaiduPCSName   = "BaiduPCS-Go"
	BdpcsOldDir    = "../../baidupcs"         //兼容文件夹
	BdpcsEnvCfgDir = "BAIDUPCS_GO_CONFIG_DIR" // https://github.com/qjfoidnh/BaiduPCS-Go/blob/main/internal/pcsconfig/pcsconfig.go#EnvConfigDir
	BdpcsCfgName   = "pcs_config.json"
	BdpcsUserINI   = "user.ini"
	BdpcsBaseDir   = "/我的资源"
)

type baiduTask struct {
	topicId  int
	url      string
	unzipPwd string
}

type BaiduCfg struct {
	Root      string // 工作目录
	Enable    bool   `ini:"enable"`    // 是否启用
	Transfer  string `ini:"transfer"`  // 转存方式: auto, manual
	Directory string `ini:"directory"` // 转存的根目录
	Bduss     string `ini:"bduss"`     // 百度网盘 bduss
	Stoken    string `ini:"stoken"`    // 百度网盘 stoken
}

type Baidu struct {
	cfg     BaiduCfg
	root    string
	program string
	mutex   *sync.Mutex
	tasks   chan baiduTask
	cron    *cron.Cron
	holder  *PanHolder
}

func NewBaidu(cfg BaiduCfg) *Baidu {
	b := &Baidu{
		cfg:   cfg,
		mutex: &sync.Mutex{},
		cron:  cron.New(cron.WithLocation(TIME_LOC)),
	}
	if b.cfg.Directory == "" {
		b.cfg.Directory = BdpcsBaseDir
	}
	return b
}

func (b Baidu) Name() string {
	return "baidu"
}

func (b Baidu) Support(pmd PanMetadata, transferType string) bool {
	return b.cfg.Transfer == transferType && strings.Contains(pmd.URL, "pan.baidu.com")
}

func (b *Baidu) SetHolder(holder *PanHolder) {
	b.holder = holder
}

func (b *Baidu) initPath() error {
	wd, e := os.Getwd()
	if e != nil {
		return fmt.Errorf("获取当前工作目录出现问题: %s", e.Error())
	}
	root := b.cfg.Root
	if root == "" {
		return fmt.Errorf("请设置百度网盘配置目录")
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

	c := filepath.Join(b.root, BdpcsCfgName)
	if !IsExist(c) {
		oc := filepath.Join(b.root, BdpcsOldDir, BdpcsCfgName)
		if IsExist(oc) {
			if e := os.Rename(oc, c); e != nil {
				b.mutex.Unlock()
				return fmt.Errorf("BaiduPan: 移动配置文件 %s 到 %s 出现问题: %s", oc, c, e.Error())
			}
		}
	}

	b.mutex.Unlock()

	if e := b.update(); e != nil {
		return e
	}

	if v, e := b.version(); e != nil {
		return e
	} else {
		log.Printf("BaiduPan: 版本号: %s\n", v)
	}

	if b.tasks == nil {
		b.mutex.Lock()
		if b.tasks == nil {
			b.tasks = make(chan baiduTask, 99)
			go func() {
				log.Println("BaiduPan: 启动任务处理协程")
				for task := range b.tasks {
					if e := b.doTransfer(task); e != nil {
						log.Println(e.Error())
						b.holder.Send(e.Error())
					}
				}
			}()

			b.cron.AddFunc("0 2 * * *", func() { //每天凌晨2点更新
				if e := b.update(); e != nil {
					log.Println(e.Error())
				}
			})
			b.cron.Start()
		}
		b.mutex.Unlock()
	}

	return b.login()
}

func (b *Baidu) isExist(file string) bool {
	// https://github.com/qjfoidnh/BaiduPCS-Go/blob/main/internal/pcscommand/meta.go#RunGetMeta
	if v, e := b.execute("meta", file); e != nil {
		log.Printf("BaiduPan: cd %s 出现问题: %s", file, e.Error())
		return false
	} else {
		return strings.Contains(v, "app_id") && strings.Contains(v, "fs_id")
	}
}

func (b *Baidu) safeCd(dir string) error {
	if !b.isExist(dir) {
		// https://github.com/qjfoidnh/BaiduPCS-Go/blob/main/internal/pcscommand/rm_mkdir.go#RunMkdir
		if v, e := b.execute("mkdir", dir); e != nil {
			return fmt.Errorf("BaiduPan: mkdir %s 出现问题: %s", dir, e.Error())
		} else {
			log.Printf("BaiduPan: mkdir %s\n", v)
		}
	}

	if v, e := b.execute("cd", dir); e != nil {
		return fmt.Errorf("BaiduPan: cd %s 出现问题: %s", dir, e.Error())
	} else {
		log.Printf("BaiduPan: cd %s , %s\n", dir, v)
	}

	// https://github.com/qjfoidnh/BaiduPCS-Go/blob/main/main.go#pwd
	if v, e := b.execute("pwd"); e != nil {
		return fmt.Errorf("BaiduPan: pwd 出现问题: %s", e.Error())
	} else {
		if v != dir {
			return fmt.Errorf("BaiduPan: cd 失败, 目录不正确, %s != %s", v, dir)
		}
	}

	return nil
}

// https://github.com/qjfoidnh/BaiduPCS-Go/blob/main/internal/pcscommand/ls_search.go#RunLs
func (b *Baidu) Ls(dir string) ([]string, error) {
	if v, e := b.execute("ls", dir); e != nil {
		return nil, fmt.Errorf("BaiduPan: ls %s 出现问题: %s", dir, e.Error())
	} else {
		text := strings.TrimSpace(v)
		if !strings.HasPrefix(text, "当前目录:") {
			return nil, fmt.Errorf("BaiduPan: ls %s 返回错误: %s", dir, text)
		}

		names := make([]string, 0)

		lines := strings.Split(text, "\n")
		if len(lines) > 1 {
			start := false
			re := regexp.MustCompile(`\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}\s+(.+?)\s*$`)
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if len(line) == 0 {
					continue
				}
				if strings.HasPrefix(line, "----") {
					start = !start
					continue
				}
				if !start {
					continue
				}
				if strings.HasPrefix(line, "#") {
					// table header
					continue
				}
				if line[0] >= '0' && line[0] <= '9' {
					m := re.FindStringSubmatch(line)
					if len(m) > 1 {
						name := m[1]
						if name != "" {
							names = append(names, name)
						}
					}
				}
			}
		}
		return names, nil
	}
}

func (b *Baidu) safeDelete(dir string) error {
	fs, e := b.Ls(dir)
	if e != nil {
		return e
	}
	if len(fs) > 0 {
		return fmt.Errorf("BaiduPan: 目录 %s 不为空, 请先清空", dir)
	}

	// https://github.com/qjfoidnh/BaiduPCS-Go/blob/main/internal/pcscommand/rm_mkdir.go#RunRemove
	if v, e := b.execute("rm", dir); e != nil {
		return fmt.Errorf("BaiduPan: rm %s 出现问题: %s", dir, e.Error())
	} else {
		log.Printf("BaiduPan: rm %s , %s\n", dir, v)
	}
	return nil
}

// https://github.com/qjfoidnh/BaiduPCS-Go/blob/main/internal/pcscommand/transfer.go#RunShareTransfer
func (b *Baidu) doTransfer(task baiduTask) error {
	log.Printf("BaiduPan: 处理转存任务: %d, %s\n", task.topicId, task.url)
	dir := fmt.Sprintf("%s/%d", b.cfg.Directory, task.topicId)

	if e := b.safeCd(dir); e != nil {
		b.safeDelete(dir)
		return e
	}

	url := task.url
	if v, e := b.execute("transfer", url); e != nil {
		return fmt.Errorf("BaiduPan: transfer %s 出现问题: %s", url, e.Error())
	} else {
		if strings.Contains(v, "成功") {
			log.Printf("BaiduPan: transfer %s 成功, %s\n", url, v)
		} else {
			log.Printf("BaiduPan: transfer %s 失败, %s\n", url, v)
			b.safeDelete(dir)
		}
	}

	pwd := task.unzipPwd
	if pwd != "" {
		if !b.isExist(PAN_PWD_FILE) {
			f := filepath.Join(os.TempDir(), PAN_PWD_FILE)
			defer os.Remove(f)

			if e := os.WriteFile(f, []byte(pwd), COMMON_FILE_MODE); e != nil {
				log.Printf("BaiduPan: 写入解压密码文件 %s 出现问题: %s", f, e.Error())
			} else {
				b.upload(f, dir)
			}
		}
	}

	log.Printf("BaiduPan: 处理转存任务完成: %d, %s\n", task.topicId, task.url)
	return nil
}

func (b *Baidu) Transfer(topicId int, md PanMetadata) error {
	uap := md.URL
	if !strings.Contains(md.URL, "?pwd=") {
		if md.Tqm == "" {
			return fmt.Errorf("BaiduPan: 缺少提取码")
		}
		uap += "?pwd=" + md.Tqm
	} else {
		// 去除字符串末尾所有不为字母数字的字符
		re := regexp.MustCompile(`[^a-zA-Z0-9]+$`)
		uap = re.ReplaceAllString(uap, "")
	}

	b.tasks <- baiduTask{
		topicId:  topicId,
		url:      uap,
		unzipPwd: md.Pwd,
	}

	return nil
}

// https://github.com/qjfoidnh/BaiduPCS-Go/blob/main/internal/pcsconfig/maniper.go#SetupUserByBDUSS
func (b *Baidu) login() error {
	needLogin := true
	if who, e := b.execute("who"); e != nil {
		return fmt.Errorf("BaiduPan: who 出现问题: %s", e.Error())
	} else {
		re := regexp.MustCompile(`uid:\s+(\d+),\s+用户名:\s+([^,]*),`)
		match := re.FindStringSubmatch(who)
		if len(match) > 1 {
			uid := match[1]
			if uid != "" && uid != "0" {
				username := match[2]
				log.Printf("BaiduPan: 登录成功, uid: %s, 用户名: %s\n", uid, username)
				needLogin = false
			}
		}
	}
	if needLogin {
		bduss := b.cfg.Bduss
		stoken := b.cfg.Stoken

		fpu := filepath.Join(b.root, BdpcsUserINI)
		if !IsExist(fpu) {
			oldDir := filepath.Join(b.root, BdpcsOldDir)
			oldIni := filepath.Join(oldDir, BdpcsUserINI)
			if IsExist(oldIni) {
				os.WriteFile(filepath.Join(oldDir, "warning.txt"), []byte("配置现在转移到 data/pan/config.ini 中, 后续本版此文件夹不再受支持"), COMMON_FILE_MODE)
				fpu = oldIni
			}
		}
		if IsExist(fpu) {
			user, e := ini.Load(fpu)
			if e != nil {
				return fmt.Errorf("BaiduPan: 读取配置文件 %s 出现问题: %s", fpu, e.Error())
			}
			bduss = user.Section("").Key("bduss").String()
			stoken = user.Section("").Key("stoken").String()
		} else {
			user := ini.Empty()
			user.Section("").Key("bduss").SetValue(bduss)
			user.Section("").Key("stoken").SetValue(stoken)
			fp := filepath.Join(b.root, BdpcsUserINI)
			if e := user.SaveTo(fp); e != nil {
				return fmt.Errorf("BaiduPan: 创建配置文件 %s 出现问题: %s", fp, e.Error())
			}
		}

		if bduss == "" || stoken == "" {
			return fmt.Errorf("BaiduPan: 请设置 bduss 和 stoken")
		}
		if v, e := b.execute("login",
			fmt.Sprintf("-bduss=%s", bduss),
			fmt.Sprintf("-stoken=%s", stoken)); e != nil {
			return fmt.Errorf("BaiduPan: login 出现问题: %s", e.Error())
		} else {
			log.Printf("BaiduPan: %s\n", v)
			if !strings.Contains(v, "登录成功") {
				return fmt.Errorf("BaiduPan: login 失败: %s", v)
			}
		}
	}
	return nil
}

// https://github.com/qjfoidnh/BaiduPCS-Go/blob/main/internal/pcscommand/upload.go#RunUpload
func (b *Baidu) upload(file, dir string) error {
	if !IsExist(file) {
		return fmt.Errorf("BaiduPan: 文件 %s 不存在", file)
	}
	if v, e := b.execute("upload", file, dir); e != nil {
		return fmt.Errorf("BaiduPan: upload %s 出现问题: %s", file, e.Error())
	} else {
		log.Printf("BaiduPan: upload %s 输出: %s\n", file, v)
	}
	return nil
}

func (b *Baidu) version() (string, error) {
	out, e := b.execute("-v")
	if e != nil {
		return "", fmt.Errorf("BaiduPan: version 出现问题: %s", e.Error())
	}
	return out, nil
}

// https://github.com/qjfoidnh/BaiduPCS-Go/blob/main/internal/pcsupdate/pcsupdate.go#CheckUpdate
func (b *Baidu) update() error {
	out, e := b.execute("update", "-y")
	if e != nil {
		return fmt.Errorf("BaiduPan: update 出现问题: %s", e.Error())
	}
	log.Printf("BaiduPan: update 输出: %s\n", out)
	return nil
}

func (b *Baidu) execute(args ...string) (string, error) {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	cmd := exec.Command(b.program, args...)
	cmd.Dir = b.root
	cmd.Env = append(os.Environ(), fmt.Sprintf("%s=%s", BdpcsEnvCfgDir, b.root))
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if e := cmd.Run(); e != nil {
		if e, ok := e.(*exec.ExitError); ok {
			log.Printf("BaiduPan: 执行返回非零退出状态: %s\n", e)
			return strings.TrimSpace(out.String()), nil
		}
		return strings.TrimSpace(out.String()), e
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
	b.cron.Stop()
	return nil
}
