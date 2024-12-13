package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/i2534/ngamm/mgr"
	"github.com/jessevdk/go-flags"
)

var (
	buildTime string
	gitHash   string
)

type Option struct {
	Port    int    `short:"p" long:"port" description:"端口号" default:"5842"`
	Program string `short:"m" long:"program" description:"ngapost2md 程序路径" default:"ngapost2md/main"`
	Token   string `short:"t" long:"token" description:"设置一个简单的访问令牌, 如果不设置则不需要令牌"`
	Version bool   `short:"v" long:"version" description:"显示版本信息"`
}

func main() {
	var opts Option
	parser := flags.NewParser(&opts, flags.Default)
	_, err := parser.Parse()
	if err != nil {
		// 如果解析失败，打印帮助信息
		if flagsErr, ok := err.(*flags.Error); ok && flagsErr.Type == flags.ErrHelp {
			os.Exit(0)
		} else {
			log.Fatalln("参数解析出现问题:", err.Error())
		}
	}

	if opts.Version {
		fmt.Printf("%s @ %s\n", gitHash, buildTime)
		os.Exit(0)
	}

	wd, err := os.Getwd()
	if err != nil {
		log.Fatalln("获取当前工作目录出现问题:", err.Error())
	}

	program := filepath.Join(wd, opts.Program)
	if _, err := os.Stat(program); os.IsNotExist(err) {
		log.Fatalln("ngapost2md 程序文件不存在:", program)
	}

	client, err := mgr.InitNGA(program)
	if err != nil {
		log.Fatalln("初始化 NGA 客户端出现问题:", err.Error())
	}

	addr := fmt.Sprintf(":%d", opts.Port)
	token := opts.Token
	if token != "" {
		log.Printf("设置访问令牌: %s\n", token)
	}
	srv, err := mgr.NewServer(&mgr.Config{Addr: addr, Token: token}, client)
	if err != nil {
		log.Fatalln("初始化服务器出现问题:", err.Error())
	}
	srv.Run()
}
