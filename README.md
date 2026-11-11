<p align="center"><img src="assets/app-icon.svg" width="96" alt="Framely 图标"></p>

# Framely · 取景

跨平台截图、标注、贴图、长截图、录屏与离线文字识别工具，使用 Go + MyGo 构建。原名 ScreenPin，v0.2.3 之后改名，GitHub 仓库也已从 `wildalley/screenpin` 改为 `wildalley/framely`（旧链接自动跳转）；可执行文件仍为 `screenpin`，配置与授权在升级后保留。启动后驻留托盘，每次截图、粘贴或打开图片都会创建独立窗口；关闭窗口后释放该图片和编辑历史。

Framely is an open-source screenshot, annotation, pinning and offline OCR app built with MyGo native UI for Windows, Linux and macOS. Scrolling capture is available on Windows/Linux; region recording and GIF export are available in v0.3.0.

![Framely 原生截图编辑器，使用生成的示例内容](docs/images/framely.png)

[下载 Release](https://github.com/wildalley/framely/releases/latest) · [更新日志](CHANGELOG.md) · [开发与构建](docs/NATIVE.md) · [OCR 流程](docs/OCR_WORKFLOW.md)

## 功能

- **截图与贴图**：区域截图、粘贴和导入图片；多窗口独立标注、缩放和关闭；钉住后只留图片置于最前，悬停显示复制、缩放、关闭。
- **标注编辑**：画笔、荧光笔、矩形、椭圆、箭头、文字和马赛克；调整颜色、粗细、透明度和圆角，支持撤销与重做。
- **滚动长截图**：Linux 与 Windows 支持框选后连续拼接、暂停、撤回和完成；采集组件只传输选区像素，自动滚动以桌面授权能力为准。
- **区域录屏**：30/60 fps 无声 MP4，可沿用本次截图选区，也可一键录制上次区域；记住帧率和分辨率，自动保存到「视频/Framely」（可修改）；录制区域旁的浮动控制条和托盘都显示计时，可暂停、继续与结束。录完或从托盘「视频转 GIF…」按起点、时长、宽度和帧率导出 GIF，显示进度。平台实测范围见 [录屏说明](docs/RECORDING.md)。
- **离线 OCR**：内置中英文模型，无需安装 Python；从托盘“框选取字…”或 `screenpin --ocr` 直接识别，长图逐块显示结果。
- **“大爆炸”式选词**：中文分词、单字模式、点击或拖动选词、Shift 连选、反选和全文编辑，按原文顺序组合复制。
- **翻译**：托盘「框选翻译…」、选区操作栏「翻译」或命令行 `--translate` 识别后直接翻译，取字窗口也可切到「翻译」页，原文和译文左右对照。可选本机离线模型（中英互译，每个方向约 110 MB，在设置中点「下载」后才下载并校验，不联网翻译），或 DeepL、百度翻译、腾讯翻译君、任意兼容 OpenAI 的大模型接口（填写自己的密钥，只发送识别出的文字，不发送图片，密钥保存在系统密钥库）。未设置时不发送任何内容。
- **取色与重复截图**：托盘「取色器…」或 `screenpin --color` 在屏幕上取色，点击复制 `#RRGGBB`；「重复上次区域」或 `screenpin --repeat` 不用框选，直接按上次确认的区域再截一张。
- **图片导出**：复制到剪贴板，或保存为 PNG、JPEG、WebP；PNG/WebP 支持透明度，JPEG 使用白色背景。

截图和 OCR 在本机处理，不上传图片，不调用云端识别 API。会联网的只有两件事，都需要你主动操作：在设置中点「下载」离线翻译模型，以及选择在线翻译服务后翻译识别出的文字。连续 OCR 会复用模型，空闲 45 秒后回收进程。内存与性能测量范围见 [资源与性能检查](docs/AUDIT_2026-11-11.md)。

## 下载与安装

当前版本为 **0.3.0**，使用 MyGo **0.8.3**。下载以 [Release 页面](https://github.com/wildalley/framely/releases) 为准。安装包使用 `framely-*` 名称，Arch 包名为 `framely`（自动替换 `screenpin-native`）；命令仍为 `screenpin`。完整包包含采集、录屏和离线 OCR 运行时，升级前请退出旧实例并完整替换配套文件。

| 平台 | 安装包或便携包 |
| --- | --- |
| Windows x64 | `framely-0.3.0-windows-amd64-setup.exe` 或 `framely-0.3.0-windows-amd64-portable.zip` |
| macOS 15+ Apple Silicon | `framely-0.3.0-darwin-arm64.dmg` |
| macOS 15+ Intel | `framely-0.3.0-darwin-amd64.dmg` |
| Ubuntu/Debian x64 | `framely-0.3.0-linux-amd64.deb` |
| Arch Linux x64 | `framely-0.3.0-1-x86_64.pkg.tar.zst` |
| 其他 Linux x64 | `framely-0.3.0-linux-amd64-portable.tar.gz` |

Ubuntu/Debian 使用 `sudo apt install ./framely-0.3.0-linux-amd64.deb`；Arch 使用 `sudo pacman -U ./framely-0.3.0-1-x86_64.pkg.tar.zst`。Linux 便携包解压后运行 `./screenpin`，或执行 `./install.sh` 安装到当前用户目录。Release 中的 `SHA256SUMS` 可用于校验下载文件。

| 平台 | 使用条件 |
| --- | --- |
| Linux | GTK3、AppIndicator、PipeWire 与适合当前桌面的 `xdg-desktop-portal` 后端；无需 WebKitGTK。发行包以 Ubuntu 22.04 或兼容运行库为基线。 |
| Windows | 长截图使用 Windows Graphics Capture，要求 Windows 10 1903+；浮动控制窗口排除采集需要 Windows 10 2004+。 |
| macOS | 支持截图、标注、贴图和 OCR，暂不支持长截图。 |

Windows/macOS 包暂未配置正式开发者签名与公证。更多平台限制见 [v0.3.0 发布说明](docs/releases/v0.3.0.md)。

## 快速使用

1. 启动 Framely，从托盘或截图快捷键框选。松开后选区仍可拖动、拉伸和微调，下方操作栏直接选择复制（Enter）、钉住（双击）、标注、保存、长截图、录屏或取字，全部沿用这一个选区。放大镜显示颜色值，按 C 复制；R 恢复上次区域。
2. 默认全局截图键：Windows **Win+Shift+S**、macOS **⌘+⇧+S**、Linux **Super+Shift+S**。设置页以独立键帽显示；Wayland 点击“配置系统快捷键”，以系统实际分配为准；已有配置保留原来的快捷键。
3. 在图片窗口标注后，按 `Ctrl/Cmd+C` 复制、`Ctrl/Cmd+S` 保存、`Ctrl/Cmd+W` 关闭。
4. 长截图从托盘或图片工具栏进入，也可在图片窗口按 `Ctrl/Cmd+Shift+L`；可在设置中配置独立的全局长截图键（推荐 `Super+Shift+L`），再次按下完成；截图快捷键也可用于完成。
5. 录屏可从截图工具栏直接沿用本次选区；托盘「录制上次区域」或录屏快捷键可跳过重复选区，录制中再次按快捷键结束保存。「录屏设置…」可调整参数或重新选区。选区、帧率和分辨率在重启后保留。录完可转 GIF。
6. OCR 从托盘“框选取字…”进入，或在图片窗口按 `Ctrl/Cmd+Shift+O` 识别整图。

```bash
screenpin                    # 启动或唤起托盘实例
screenpin --capture          # 截图
screenpin --scroll           # 长截图（Linux / Windows）
screenpin --ocr              # 直接框选取字
screenpin --color            # 取色器：点击复制颜色值（Linux / Windows）
screenpin --repeat           # 按上次区域再截一张（Linux / Windows）
screenpin --record           # 开始录屏（沿用上次区域）；录制中再次调用结束保存
screenpin ./example.png      # 打开图片
```

Linux Wayland 的截图、屏幕共享与快捷键授权由系统门户分别管理。niri 等桌面的设置方式、权限问题及全部快捷键见 [使用说明](docs/NATIVE.md)。

配置位于 Linux 的 `~/.config/screenpin/config.json`（或 `$XDG_CONFIG_HOME` 下）、Windows 的 `%APPDATA%\screenpin\config.json`、macOS 的 `~/Library/Application Support/screenpin/config.json`。建议通过设置面板修改。

## 图标与品牌

新版使用“折叠画面”F 标记：深青底、暖白纸片与橙色折角。应用、托盘及界面标记都有可编辑 SVG 母版，多尺寸 PNG 可用 `python3 scripts/render-icons.py` 重新生成（需要 librsvg 的 `rsvg-convert`）。设计来源及提示词见 [品牌说明](docs/BRAND.md)。

## 开发与构建

需要 Go 1.27.3+。完整发行包还需要 `uv`，构建脚本会准备 Python 3.12 环境。Linux 采集组件需要 C 编译器、`pkg-config` 和 PipeWire 开发头文件；媒体组件另需 bash、make、pkg-config 和 NASM（x64 性能构建）；Linux 另需 VAAPI 开发库。Windows 需要 Visual Studio x64 Native Tools、Windows SDK 与 MSYS2；macOS 使用 Xcode。本机工具安装与完整步骤见 [录屏构建说明](docs/RECORDING.md)。MyGo 由 `go.mod` 的工具依赖管理。

```bash
# Go 测试与静态检查
go test -race ./...
go vet ./...

# 构建当前平台的原生采集与离线 OCR 组件
python3 scripts/build-components.py

# Linux x64 完整包示例
go tool mygo build -platform linux/amd64 -skip-installer -skip-dmg -skip-notarize
python3 scripts/package-release.py
```

组件生成在 `resources/<系统>-<架构>/workers/`，应用包位于 `build/native/`，最终发行附件位于 `build/release/`。各平台组件须在目标系统构建；仅交叉编译 Go 程序不会得到完整 OCR 发行包。

开发运行时显式设置组件目录：

```bash
export SCREENPIN_COMPONENTS_DIR="$PWD/resources/$(go env GOOS)-$(go env GOARCH)/workers"
go run ./cmd/screenpin
```

完整构建、集成测试和发布步骤见 [原生开发说明](docs/NATIVE.md)。问题反馈请附操作系统、桌面环境、Framely 版本和复现步骤，避免上传含隐私的截图。

## 仓库结构

| 目录 | 内容 |
| --- | --- |
| `cmd/`、`internal/` | Go 应用、界面、图像处理、平台集成和测试 |
| `workers/` | 原生采集组件与离线 OCR 运行时 |
| `assets/` | 应用、托盘和安装图标 |
| `scripts/`、`packaging/arch-native/` | 构建、校验、打包与安装脚本 |
| `docs/` | 使用说明、测试记录和发行说明 |

当前主线仅维护 Go + MyGo 原生版；旧 Tauri/React 源码可在 `v0.1.1` 标签和 Git 历史中查阅。后续计划见 [改进记录](IMPROVEMENTS.md)。

## 许可证

项目采用 [MIT](LICENSE) 许可证。分词词典及图标的第三方许可证保留在各自目录；发行包附带 OCR 依赖许可证与 Python 依赖清单。新增录屏组件采用精简 LGPL FFmpeg；随包提供对应源码、构建配置和许可证，详见录屏说明。
