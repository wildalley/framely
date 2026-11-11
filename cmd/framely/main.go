package main

import (
	"errors"
	"flag"
	"fmt"
	"github.com/wildalley/framely/internal/brand"
	"os"

	"github.com/wildalley/framely/internal/desktop"
)

const usage = `Framely 取景 — 截图、贴图、长截图、录屏、离线文字识别与翻译

用法: framely [--hidden | --capture | --scroll | --ocr | --translate | --color | --repeat | --record] [图片]
  默认启动到托盘；每张图片独立开窗，关闭即销毁
  --hidden     在后台启动；已有实例时不打扰当前窗口
  --capture    截图；已有实例时交给该实例
  --scroll     打开长截图；已有实例时交给该实例
  --ocr        框选屏幕后直接取字
  --translate  框选屏幕，识别文字后直接翻译
  --color      取色器：点击复制颜色值
  --repeat     按上次确认的区域再截一张
  --record     打开录屏；录制中再次调用则结束并保存
  --version    显示版本
  --help       显示帮助
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
		fmt.Println(brand.Name + " " + desktop.Version + " (mygo)")
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
		fmt.Fprintln(os.Stderr, brand.Name+":", err)
		os.Exit(1)
	}
}
