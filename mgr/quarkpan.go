package mgr

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/i2534/ngamm/mgr/log"

	"gopkg.in/ini.v1"
)

var (
	QuarkUserINI = "user.ini"
	QuarkBaseDir = "来自：分享"
)

type quarkTask struct {
	topicId int
	record  TransferRecord
	opt     PanOpt
}

type QuarkCfg struct {
	Root      string // 工作目录
	Enable    bool   `ini:"enable"`    // 是否启用
	Transfer  string `ini:"transfer"`  // 转存方式: auto, manual
	Directory string `ini:"directory"` // 转存的根目录
	Cookie    string `ini:"cookie"`    // 夸克网盘 cookie
}

type QuarkPan struct {
	cfg    QuarkCfg
	root   string
	quark  *Quark
	mutex  *sync.Mutex
	tasks  chan quarkTask
	holder *PanHolder
}

func NewQuarkPan(cfg QuarkCfg) *QuarkPan {
	q := &QuarkPan{
		cfg:   cfg,
		mutex: &sync.Mutex{},
	}
	if q.cfg.Directory == "" {
		q.cfg.Directory = QuarkBaseDir
	}
	return q
}

func (q *QuarkPan) Name() string {
	return "quark"
}

func (q *QuarkPan) SetHolder(holder *PanHolder) {
	q.holder = holder
}

func (q *QuarkPan) Init() error {
	q.mutex.Lock()
	defer q.mutex.Unlock()

	if q.root == "" {
		wd, e := os.Getwd()
		if e != nil {
			return fmt.Errorf("获取当前工作目录出现问题: %s", e.Error())
		}
		root := q.cfg.Root
		if root == "" {
			return fmt.Errorf("请设置夸克网盘配置目录")
		}
		root = JoinPath(wd, root)
		q.root = root
	}

	cookie := q.cfg.Cookie
	if cookie == "" {
		fp := filepath.Join(q.root, QuarkUserINI)
		cfg, e := ini.Load(fp)
		if e != nil {
			return fmt.Errorf("QuarkPan: 读取配置文件 %s 出现问题: %s", fp, e.Error())
		}
		cookie = cfg.Section("").Key("cookie").String()
	}

	quark := NewQuark(cookie)
	info := quark.Init()
	if info == nil {
		return fmt.Errorf("QuarkPan: 初始化失败")
	}

	if !quark.IsActive {
		return fmt.Errorf("QuarkPan: 用户未处于登录状态")
	}

	q.quark = quark
	log.Printf("QuarkPan: 登录成功, 用户名: %s\n", q.quark.Nickname)

	if q.tasks == nil {
		q.tasks = make(chan quarkTask, 99)
		go func() {
			log.Println("QuarkPan: 启动任务处理协程")
			for task := range q.tasks {
				var e error
				var status string
				if task.opt == PAN_OPT_DELETE {
					dir := q.topicDir(task.topicId)
					getFids := q.quark.GetFids([]string{dir})
					if len(getFids) > 0 {
						toPdirFid := getFids[0]["fid"].(string)
						q.quark.Delete([]string{toPdirFid})
					}
					status = TRANSFER_STATUS_PENDING
				} else if task.opt == PAN_OPT_SAVE {
					e = q.doTransfer(task)
					status = TRANSFER_STATUS_SUCCESS
				}
				if e != nil {
					q.holder.notify(task.topicId, task.record.URL, TRANSFER_STATUS_FAILED, e.Error())
				} else {
					q.holder.notify(task.topicId, task.record.URL, status, "")
				}
			}
		}()
	}

	return nil
}

func (q QuarkPan) Support(record TransferRecord) bool {
	return strings.Contains(record.URL, "pan.quark.cn")
}
func (q QuarkPan) TransferType() string {
	return q.cfg.Transfer
}

type quarkFile struct {
	fid      string
	fidToken string
	size     float64
}

func (q *QuarkPan) topicDir(topicId int) string {
	return fmt.Sprintf("%s/%d", q.cfg.Directory, topicId)
}

func (q *QuarkPan) doTransfer(task quarkTask) error {
	quark := q.quark
	url := task.record.URL
	if !strings.Contains(url, "?pwd=") && task.record.Tqm != "" {
		url = fmt.Sprintf("%s?pwd=%s", url, task.record.Tqm)
	}
	log.Group(groupPan).Printf("QuarkPan: 处理转存任务: %d, %s\n", task.topicId, url)
	// copy from DoSaveCheck
	pwdID, passcode, pdirFid := quark.GetIDFromURL(url)
	isSharing, stoken := quark.GetStoken(pwdID, passcode)
	if !isSharing {
		return fmt.Errorf("QuarkPan: %s 不是分享链接", url)
	}

	shareDetail := quark.GetDetail(pwdID, stoken, pdirFid, 0)
	shareFileList := shareDetail["list"].([]any)

	fs := make(map[string]quarkFile)
	for _, file := range shareFileList {
		fileMap := file.(map[string]any)

		ban := fileMap["ban"].(bool)
		name := fileMap["file_name"].(string)
		if ban {
			log.Group(groupPan).Printf("QuarkPan: 文件 %s 被 Ban, 跳过", name)
			continue
		}
		fs[name] = quarkFile{
			fid:      fileMap["fid"].(string),
			fidToken: fileMap["share_fid_token"].(string),
			size:     fileMap["size"].(float64),
		}
	}
	if len(fs) == 0 {
		return fmt.Errorf("QuarkPan: 分享链接 %s 中没有有效文件", url)
	}

	// 获取目标目录FID
	dir := q.topicDir(task.topicId)
	var toPdirFid string

	getFids := quark.GetFids([]string{dir})
	if len(getFids) > 0 {
		toPdirFid = getFids[0]["fid"].(string)
	} else {
		mkdirResult := quark.Mkdir(dir)
		if mkdirResult["code"].(float64) == 0 {
			toPdirFid = mkdirResult["data"].(map[string]any)["fid"].(string)
		} else {
			return fmt.Errorf("QuarkPan: 创建目录 %s 失败, %s", dir, mkdirResult["message"].(string))
		}
	}

	dirFileList := quark.LsDir(toPdirFid, 0)
	for _, file := range dirFileList {
		fileName := file["file_name"].(string)
		if f, ok := fs[fileName]; ok {
			if f.size == file["size"].(float64) {
				log.Group(groupPan).Printf("QuarkPan: 文件 %s 已存在, 跳过", fileName)
				delete(fs, fileName)
			}
		}
	}
	if len(fs) == 0 {
		return fmt.Errorf("QuarkPan: 文件都已存在")
	}

	var fidList []string
	var fidTokenList []string
	for _, v := range fs {
		fidList = append(fidList, v.fid)
		fidTokenList = append(fidTokenList, v.fidToken)
	}
	// 保存文件
	saveFile := quark.SaveFile(fidList, fidTokenList, toPdirFid, pwdID, stoken)
	if saveFile["code"].(float64) != 0 {
		if len(dirFileList) == 0 { // 保存前为空目录, 保存又失败, 再次判断目录是否为空
			if dirFileList = quark.LsDir(toPdirFid, 0); len(dirFileList) == 0 {
				log.Group(groupPan).Printf("QuarkPan: 目录 %s 为空, 删除", dir)
				// 删除目录
				quark.Delete([]string{toPdirFid})
			}
		}
		return fmt.Errorf("QuarkPan: 保存文件失败, %s", saveFile["message"].(string))
	}

	log.Group(groupPan).Printf("QuarkPan: 处理转存任务完成: %d, %s\n", task.topicId, url)
	return nil
}

func (q *QuarkPan) Ls(dir string) (any, error) {
	quark := q.quark
	fids := quark.GetFids([]string{dir})
	if len(fids) == 0 {
		return nil, fmt.Errorf("QuarkPan: 目录 %s 不存在", dir)
	}
	fid := fids[0]["fid"].(string)
	list := quark.LsDir(fid, 0)
	return list, nil
}

func (q *QuarkPan) Transfer(topicId int, record TransferRecord) error {
	if !strings.Contains(record.URL, "?pwd=") && record.Tqm == "" {
		log.Println("QuarkPan: 提取码为空")
	}
	q.tasks <- quarkTask{
		topicId,
		record,
		PAN_OPT_SAVE,
	}
	return nil
}

func (q *QuarkPan) Operate(topicId int, record *TransferRecord, opt PanOpt) error {
	q.tasks <- quarkTask{
		topicId,
		*record,
		opt,
	}
	return nil
}

func (q *QuarkPan) Close() error {
	if q.tasks != nil {
		close(q.tasks)
		q.tasks = nil
	}
	return nil
}
