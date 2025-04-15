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

type Baidu struct {
	cfg     BaiduCfg
	root    string
	program string
	mutex   *sync.Mutex
	tasks   chan baiduTask
	cron    *cron.Cron
}

func NewBaidu(cfg BaiduCfg) *Baidu {
	return &Baidu{
		cfg:   cfg,
		mutex: &sync.Mutex{},
		cron:  cron.New(cron.WithLocation(TIME_LOC)),
	}
}

func (b Baidu) Name() string {
	return "baidu"
}

func (b Baidu) Support(pmd PanMetadata) bool {
	return strings.Contains(pmd.URL, "pan.baidu.com")
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

// https://github.com/qjfoidnh/BaiduPCS-Go/blob/main/internal/pcscommand/transfer.go#RunShareTransfer
func (b *Baidu) doTransfer(task baiduTask) error {
	log.Printf("BaiduPan: 处理转存任务: %d, %s\n", task.topicId, task.url)
	dir := fmt.Sprintf("%s/%d", BdpcsBaseDir, task.topicId)

	// https://github.com/qjfoidnh/BaiduPCS-Go/blob/main/internal/pcscommand/rm_mkdir.go#RunMkdir
	if v, e := b.execute("mkdir", dir); e != nil {
		return fmt.Errorf("BaiduPan: mkdir %s 出现问题: %s", dir, e.Error())
	} else {
		// if strings.Contains(v, "成功") {
		log.Printf("BaiduPan: mkdir %s\n", v)
	}

	// https://github.com/qjfoidnh/BaiduPCS-Go/blob/main/internal/pcscommand/cd.go#RunChangeDirectory
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

	url := task.url
	if v, e := b.execute("transfer", url); e != nil {
		return fmt.Errorf("BaiduPan: transfer %s 出现问题: %s", url, e.Error())
	} else {
		if strings.Contains(v, "成功") {
			log.Printf("BaiduPan: transfer %s 成功, %s\n", url, v)
		} else {
			log.Printf("BaiduPan: transfer %s 失败, %s\n", url, v)
		}
	}

	pwd := task.unzipPwd
	if pwd != "" {
		f := filepath.Join(os.TempDir(), "_uzp.txt")
		defer os.Remove(f)

		if e := os.WriteFile(f, []byte(pwd), COMMON_FILE_MODE); e != nil {
			log.Printf("BaiduPan: 写入解压密码文件 %s 出现问题: %s", f, e.Error())
		} else {
			b.upload(f, dir)
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
		re := regexp.MustCompile(`uid: (\d+)`)
		match := re.FindStringSubmatch(who)
		if len(match) > 1 {
			uid := match[1]
			if uid != "" && uid != "0" {
				log.Printf("BaiduPan: login, uid: %s\n", uid)
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
