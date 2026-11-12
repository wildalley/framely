package desktop

import (
	"errors"
	"flag"
	"io"
)

const Version = "0.4.5"

type Launch struct {
	Hidden    bool
	Capture   bool
	Scroll    bool
	Record    bool
	OCR       bool
	Color     bool
	Translate bool
	Repeat    bool
	Version   bool
	File      string
}

func ParseArgs(args []string) (Launch, error) {
	var launch Launch
	flags := flag.NewFlagSet("framely", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.BoolVar(&launch.Hidden, "hidden", false, "在托盘中启动")
	flags.BoolVar(&launch.Capture, "capture", false, "截取屏幕或通知已有实例截图")
	flags.BoolVar(&launch.Scroll, "scroll", false, "打开长截图")
	flags.BoolVar(&launch.Record, "record", false, "打开录屏或结束正在进行的录屏")
	flags.BoolVar(&launch.OCR, "ocr", false, "框选屏幕并直接识别文字")
	flags.BoolVar(&launch.Color, "color", false, "从屏幕取色并复制颜色值")
	flags.BoolVar(&launch.Translate, "translate", false, "框选屏幕，识别文字并翻译")
	flags.BoolVar(&launch.Repeat, "repeat", false, "按上次的区域再截一张")
	flags.BoolVar(&launch.Version, "version", false, "显示版本")
	if err := flags.Parse(args); err != nil {
		return launch, err
	}
	if flags.NArg() > 1 {
		return launch, errors.New("一次只能打开一张图片")
	}
	count := 0
	for _, enabled := range []bool{launch.Capture, launch.Scroll, launch.OCR, launch.Record, launch.Color, launch.Repeat, launch.Translate} {
		if enabled {
			count++
		}
	}
	if count > 1 {
		return launch, errors.New("--capture、--scroll、--record、--ocr、--translate、--color 和 --repeat 不能同时使用")
	}
	if flags.NArg() == 1 {
		launch.File = flags.Arg(0)
		if launch.Capture || launch.Scroll || launch.OCR || launch.Record || launch.Color || launch.Repeat || launch.Translate {
			return launch, errors.New("打开文件和截图选项不能同时使用")
		}
	}
	return launch, nil
}
