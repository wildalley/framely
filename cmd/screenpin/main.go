package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/wildalley/screenpin/internal/desktop"
)

const usage = `ScreenPin 托盘截图工具

用法: screenpin [--hidden | --capture] [图片]
  默认启动到托盘；每张图片独立开窗，关闭即销毁
  --hidden   在后台启动；已有实例时不打扰当前窗口
  --capture  截图；已有实例时交给该实例
  --version  显示版本
  --help     显示帮助
`

func main() {
	launch, err := desktop.ParseArgs(os.Args[1:])
	if errors.Is(err, flag.ErrHelp) {
		fmt.Print(usage)
		return
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	if launch.Version {
		fmt.Println("ScreenPin " + desktop.Version + " (mygo)")
		return
	}
	if launch.File != "" {
		info, err := os.Stat(launch.File)
		if err != nil || !info.Mode().IsRegular() {
			fmt.Fprintln(os.Stderr, "无法打开文件:", launch.File)
			os.Exit(2)
		}
	}
	if err := desktop.Run(launch); err != nil {
		fmt.Fprintln(os.Stderr, "ScreenPin:", err)
		os.Exit(1)
	}
}
