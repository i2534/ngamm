package main

import (
	"fmt"
	"log"
	"os"
	"strconv"

	"github.com/i2534/ngamm/mgr"
	"github.com/jessevdk/go-flags"
)

var (
	buildTime string
	gitHash   string
	logFlags  string
)

type Option struct {
	Port    int    `short:"p" long:"port" description:"端口号" default:"5842"`
	Program string `short:"m" long:"program" description:"ngapost2md 程序路径, 相对于主程序" default:"ngapost2md/ngapost2md"`
	Token   string `short:"t" long:"token" description:"设置一个简单的访问令牌, 如果不设置则不需要令牌"`
	Smile   string `short:"s" long:"smile" description:"表情配置:\nlocal: 使用本地缓存(如果没有则自动下载)\nweb: 使用远程(即NGA服务器上的)\n" default:"local"`
	Pan     string `short:"n" long:"pan" description:"网盘配置根目录, 如果不设置则不使用网盘相关功能:\n如果设置, 在此目录下放置 config.ini 配置网盘"`
	Config  string `short:"c" long:"config" description:"配置文件路径(ini), 如果不设置则使用默认配置, 优先级: 命令行参数 > 配置文件 > 默认值"`
	Version bool   `short:"v" long:"version" description:"显示版本信息"`
}

func main() {
	log.SetOutput(os.Stdout)

	flag, fe := strconv.Atoi(logFlags)
	if fe != nil {
		flag = log.LstdFlags
	}
	log.SetFlags(flag)

	var opts Option
	parser := flags.NewParser(&opts, flags.Default)
	_, e := parser.Parse()
	if e != nil { // 如果解析失败，打印帮助信息
		if fe, ok := e.(*flags.Error); ok && fe.Type == flags.ErrHelp {
			os.Exit(0)
		} else {
			log.Fatalln("参数解析出现问题:", e.Error())
		}
	}

	if opts.Version {
		fmt.Printf("%s @ %s\n", gitHash, buildTime)
		os.Exit(0)
	}

	wd, e := os.Getwd()
	if e != nil {
		log.Fatalln("获取当前工作目录出现问题:", e.Error())
	}

	log.Printf("NGAMM 版本: %s @ %s\n", gitHash, buildTime)
	log.Println("作者: i2534 [ https://github.com/i2534/ngamm ]")

	global := &mgr.Config{}
	if opts.Config != "" {
		log.Printf("加载配置文件: %s\n", opts.Config)
		cfg, e := mgr.LoadConfig(mgr.JoinPath(wd, opts.Config))
		if e != nil {
			log.Fatalln("加载配置文件出现问题:", e.Error())
		}
		global = cfg
	}
	if opts.Port != 0 {
		global.Port = opts.Port
	}
	if opts.Program != "" {
		global.Program = opts.Program
	}
	if opts.Token != "" {
		global.Token = opts.Token
	}
	if opts.Smile != "" {
		global.Smile = opts.Smile
	}
	if opts.Pan != "" {
		global.Pan = opts.Pan
	}

	program := mgr.JoinPath(wd, global.Program)
	if !mgr.IsExist(program) {
		log.Fatalln("ngapost2md 程序文件不存在:", program)
	}

	client, e := mgr.InitNGA(program)
	if e != nil {
		log.Fatalln("初始化 NGA 客户端出现问题:", e.Error())
	}
	mgr.ChangeUserAgent(client.GetUA())

	addr := fmt.Sprintf(":%d", global.Port)
	token := global.Token
	if token != "" {
		log.Printf("设置访问令牌: %s\n", token)
	}
	if gitHash == "" {
		gitHash = "what"
	}
	srv, e := mgr.NewServer(&mgr.SrvCfg{
		Addr:    addr,
		GitHash: gitHash,
		Config:  global,
	}, client)
	if e != nil {
		log.Fatalln("初始化服务器出现问题:", e.Error())
	}

	if global.Pan != "" {
		go func() {
			if ph, e := mgr.NewPanHolder(global.Pan); e != nil {
				log.Println("初始化网盘出现问题:", e.Error())
			} else {
				srv.SetNetPan(ph)
			}
		}()
	}

	srv.Run()
}
