package desktop

import (
	"errors"
	"flag"
	"io"
)

const Version = "0.1.1"

type Launch struct {
	Hidden  bool
	Capture bool
	Version bool
	File    string
}

func ParseArgs(args []string) (Launch, error) {
	var launch Launch
	flags := flag.NewFlagSet("screenpin", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.BoolVar(&launch.Hidden, "hidden", false, "在托盘中启动")
	flags.BoolVar(&launch.Capture, "capture", false, "截取屏幕或通知已有实例截图")
	flags.BoolVar(&launch.Version, "version", false, "显示版本")
	if err := flags.Parse(args); err != nil {
		return launch, err
	}
	if flags.NArg() > 1 {
		return launch, errors.New("一次只能打开一张图片")
	}
	if flags.NArg() == 1 {
		launch.File = flags.Arg(0)
		if launch.Capture {
			return launch, errors.New("打开文件和 --capture 不能同时使用")
		}
	}
	return launch, nil
}
