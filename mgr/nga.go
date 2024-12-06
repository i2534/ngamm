package mgr

import (
	"bytes"
	"errors"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/ini.v1"
)

const (
	NGA_CFG = "config.ini"
)

type Client struct {
	program string
	version string
	root    string
	cfg     *ini.File
}

func InitNGA(program string) (*Client, error) {
	dir := filepath.Dir(program)
	client := &Client{program: program, root: dir}
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

	// 因为 post2md 使用 ini.Load("config.ini")

	client.root = dir
	client.version = version
	client.cfg = cfg
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

//	func (c *Client) CheckVersion() bool {
//		version := c.GetVersion()
//		if version == "" {
//			return false
//		}
//		return true
//	}
func (c *Client) Download(tid int) (bool, string) {
	out, err := c.execute([]string{strconv.Itoa(tid)})
	if err != nil {
		log.Printf("下载主题 %d 出现问题: %s\n", tid, err.Error())
	} else {
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
