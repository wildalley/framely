# MyGo 原生预览版

`refactor/mygo-native` 使用 Go + MyGo 实现托盘截图与贴图工具。启动时只显示托盘；每张图片独立开窗，关闭就销毁图片、标注和撤销记录。没有主页、截图历史或工程保存流程。当前原生发行版本为 `v0.2.0`，Tauri 旧版为 `v0.1.1`。

## 使用

从托盘选择「截图」「粘贴图片」或「打开图片」。已有图片继续保留，新图片在另一个窗口打开，可以同时置顶多张图片。截图期间暂时隐藏 ScreenPin 的窗口，结束后恢复仍然打开的窗口。

图片窗口只有图片和紧凑工具栏：八种标注、样式、撤销/重做、置顶、保存和复制。MyGo 工具栏自动把放不下的标注工具收入菜单，复制、保存、置顶和关闭保持可见；工具栏支持 Tab 和方向键，画布右键使用原生菜单。控件跟随系统明暗主题，标注支持移动、缩放、样式、图层以及文字原位编辑。

- `Ctrl/Cmd+C` 复制图片，`Ctrl/Cmd+S` 保存 PNG、JPEG 或 WebP；保存菜单可切换格式，并设置 JPEG / WebP 质量（60%、80%、92%、100%）。
- `Ctrl/Cmd+V` 粘贴到新的图片窗口，`Ctrl/Cmd+W` 直接关闭并销毁当前图片。
- `Esc` 依次关闭样式、取消选中并返回选择工具、关闭图片。关闭不询问未保存修改。
- 从托盘打开「快捷键与设置」或「查看按键」，不需要先截图。工具窗口关闭后也会销毁。
- 设置「复制后关闭图片」适合截完即贴；置顶图片继续保留。沿用既有 JSON 字段 `hideAfterCopy`，不会保留隐藏图片。
- 开机自启由系统管理，登录后在托盘待命；没有托盘时显示设置窗口，关闭最后一个窗口会退出，避免留下无法操作的后台实例。

支持导入和保存 PNG / JPEG / WebP。PNG 和 WebP 保留透明度，JPEG 在白色背景上合成；WebP 使用有损压缩，100% 质量也不表示无损。原生版不打开或写入 `.screenpin` 工程；如需处理旧工程，请先用 Tauri 稳定版导出图片。预览包不注册工程或图片文件关联。

Linux 首次截图使用系统选区；可从设置中点击「允许直接选区」授权。已获静默截图权限时使用 ScreenPin 自己的选区，拖动后松开截取、拖动时按住空格移动、Enter 截取整块显示器、Esc 或右键取消。权限或自有选区不可用时使用系统选区，取消不会创建空窗口。退出会取消正在等待的截图和选区。

全局截图默认 `Win+Shift+S`，可从托盘设置录制其他组合键。Wayland 下注册会通过门户请求系统授权界面，并等待实际授权结果，再显示实际分配的按键；新注册和配置保存都成功后才替换旧绑定，失败时保留旧绑定。关闭等待中的设置窗口会取消候选请求。首次启动注册失败会显示设置窗口（`--hidden` 静默启动除外），可点击「授权快捷键」重试；授权等待期间禁止重复提交。

授权界面由桌面的 GlobalShortcuts 门户实现。GNOME 等支持该接口的桌面会由门户提供授权；请求被取消、授权被拒和后端不可用都不会被当成注册成功。

### niri 快捷键配置

niri 的系统门户支持仍在[上游开发](https://github.com/niri-wm/niri/pull/4744)。当前门户注册失败时，普通启动会弹出「配置 niri 截图快捷键」确认框；也可以在设置里点「配置 niri 快捷键」。这是应用修改桌面配置前的确认，框中列出组合键、执行命令和文件。确认前不修改配置；`--hidden` 启动不会主动弹出。

确认后，ScreenPin 先备份原配置，用 `niri validate` 检查完整候选，再在 niri 配置末尾引用同目录的 `screenpin-shortcut.kdl`。按键文件执行当前安装的 ScreenPin 和 `--capture`，支持应用未启动时直接截图。已有同组合的桌面操作会由截图替换；换成其他按键时，旧组合原来的操作自动恢复。配置和设置保存失败会回滚；确认期间文件被其他程序修改时停止操作，要求重新确认。

支持默认配置路径、`NIRI_CONFIG` 与运行中 niri 的绝对 `--config` 路径，以及普通/原始字符串 include、注释、禁用节点与 Super/Alt 的 Mod 别名。配置路径为符号链接、使用无法识别的复杂语法或循环引用时，会停止自动修改并提示手工处理。

确认配置后，托盘、设置和帮助会显示按键及「niri」。绑定由桌面持久管理，退出应用后仍可用于再次启动截图。若要移除集成，删除 niri 主配置末尾 ScreenPin 标记下的 include 和对应 `screenpin-shortcut.kdl` 即可恢复原操作；备份保存在 niri 配置目录的 `screenpin-backup-*` 中。

Windows/macOS 交互、真实 Wayland 授权与多显示器选区仍需桌面验收；跨平台构建和独立 D-Bus 协议测试不能代替这些检查。

## 长截图与文字识别

从图片工具栏的「长截图」按钮、托盘「长截图…」或 `screenpin --scroll` 进入。图片窗口还支持 `Ctrl/Cmd+Shift+L` 打开长截图，`Ctrl/Cmd+Shift+S` 新截图。选择显示器/窗口后，在预览中框选滚动内容并确认。尽量避开滚动条、动画和浮层；手动模式请缓慢向下滚动，保留至少半屏重叠。可从托盘打开控制窗口暂停、撤回最后一段、继续或完成；再次按全局截图快捷键直接完成并打开图片编辑器。关闭控制窗口取消此次长截图。

工具栏第二行直接调整颜色、圆角、粗细/字号与不透明度，输入数值或使用旁边的加减按钮即可生效；输入框中方向键可微调。宽窗口还可同时调整截图圆角；窄窗口通过「目标」切换标注与截图，常用图片操作保留在底行。属性行无需滚动，输入期间不会误触画布快捷键。设置与全部按键说明合并在「设置与快捷键」，也可从图片窗口按 `Ctrl/Cmd+,`；开机自启位于「常规」页签，截图授权和屏幕共享说明位于「权限与长截图」。这些图片窗口快捷键在输入标注文字时不会触发。

形状标注画完即选中并进入调整模式，可直接拖动标注或八个控制点；再次选择工具可继续绘制。画笔、荧光笔保持连续绘制。长截图/OCR 的确认框选页也可框内拖动、拖动边缘缩放，并支持方向键移动和 Shift+方向键调整右边/下边。

矩形圆角和截图圆角可从第二行属性栏调整；「样式」弹层继续提供完整预设、连续调节、文字编辑和图层操作。两者分别提供 0–100% 圆角比例及预设，百分比表示短边一半的比例，100% 为胶囊形/最大圆角。预览、复制、OCR 和导出使用相同裁切；PNG/WebP 保留透明，JPEG 的四角为白底。圆角设置按图片独立保存于当前编辑历史，支持撤销和重做，关闭图片后释放。

Windows 采集组件使用 Windows Graphics Capture，要求 Windows 10 1903 或更新版本；自动模式在选区中心发送滚轮。目标窗口改变、鼠标移开或输入被拒时暂停。普通权限无法向管理员权限程序发送输入时，改用手动模式；应用不会自行提权。

Linux 通过系统 ScreenCast 选择器授权屏幕共享，用 PipeWire 持续采集；自动模式要求 RemoteDesktop 同时提供并授予指针控制。GNOME/KDE 是否可用以实际门户能力及授权结果为准，不能仅凭桌面名称判断。当前 niri 没有提供该指针接口，因此使用手动模式。屏幕共享与快捷键授权是独立的，授权取消会释放候选会话。截图过程中不要切换采集源的尺寸；滚动超过重叠范围、重复图案、反向或动态内容导致无法可靠匹配时暂停并保留结果。

如果 niri 点击开始后不出现来源选择器，检查用户门户配置：niri 需要 `xdg-desktop-portal-gnome` 提供屏幕共享；使用 Hyprland 后端可能使 `hyprland-share-picker` 崩溃。在 `~/.config/xdg-desktop-portal/niri-portals.conf` 的 `[preferred]` 中配置 `org.freedesktop.impl.portal.ScreenCast=gnome`，保留自己所需的其他后端设置；备份后再修改，重新登录或重启桌面门户生效。应用会诊断这一冲突，并提供重新检查入口。等待系统窗口期间可以取消，不必退出进程。

Linux 组件支持普通 CPU 缓冲区和线性 DMA-BUF；DMA-BUF 按实际步长、偏移及 fd 存储范围映射并同步，不能把其 `maxsize=1` 哨兵值解释为实际图像长度。非线性 GPU 布局不会直接当作线性像素读取。

拼接最多保留 2400 万像素、高 30000 像素；达到限制后可完成当前图片。预览使用缩略图，完成时才生成整张像素，保存仍用完整分辨率。macOS 暂未实现长截图采集组件，发行包包含离线 OCR。

图片窗口工具栏「识别文字」菜单可识别整图或框选，右键菜单也提供入口；`Ctrl/Cmd+Shift+O` 识别整图。结果窗口支持编辑全文、复制全文及逐行复制。识别处理当前标注后的画面，马赛克不读取底下原图。关闭图片或点击取消会终止 OCR；识别超时为 3 分钟，输入最多 3200 万像素。

完整 Windows/Linux/macOS 包附带 RapidOCR 3.9.2、PP-OCRv6-small ONNX CPU 模型和独立运行时，不需要安装 Python，不上传截图、不在运行时下载模型。长图分块识别并合并重叠文字；复杂多栏排版和表格暂不重建文档结构。

## 开发与检查

需要 Go 1.27.3 或更新版本。Linux 原生窗口需要 GTK3、DBus、AppIndicator 和 `xdg-desktop-portal`，不需要 WebKitGTK。Wayland 需要适合桌面环境的 portal 后端。

```bash
go run ./cmd/screenpin
go run ./cmd/screenpin --capture
go run ./cmd/screenpin ./example.png
go test -race ./...
go test -tags nodynamic ./internal/render
go vet ./...
bash scripts/test-capture.sh # Linux 原生缓冲区协议及 DMA-BUF 哨兵/步长/边界回归
```

在真实 Linux 桌面手动检查来源选择器及首帧时，可运行 `SCREENPIN_LIVE_CAPTURE=1 go test -count=1 -run TestLiveDesktopStream -v ./internal/capture`。该命令会请求真正的系统共享选择；测试不替用户授权，30 秒后取消。自动化协议测试仍使用独立总线，不会请求真实桌面。

默认启动和再次启动都只让托盘待命；`--hidden` 保留为兼容参数。`--capture` 和图片参数交给已有实例，每次打开独立图片，不替换当前图片。

Linux 门户测试使用独立会话总线，不调用当前桌面的门户。MyGo 无窗口控件回归覆盖文字输入焦点、工具栏溢出与方向键、画布菜单、关闭时的状态释放及异步结果隔离。CI 执行这些检查并构建 Linux、Windows 和 macOS 原生产物。

WebP 编码使用 `github.com/gen2brain/webp`，无需 CGo；系统 libwebp 不可用时使用随程序编译的 Go 后端。`nodynamic` 检查强制使用该后端，以独立解码器核验文件头、尺寸、透明度、裁剪行布局、质量设置和马赛克结果。

## 构建

先在目标 Windows/Linux/macOS 上构建组件（不能用 Linux 产物充当 Windows 组件）。构建机需要 `uv`；脚本创建隔离的 Python 3.12 环境并下载依赖，最终使用者无需 Python。Linux 需要 C 编译器、pkg-config、PipeWire 开发头文件；Windows 需要 Visual Studio 的 x64 Native Tools 环境和含 C++/WinRT 的 Windows SDK。

```bash
python scripts/build-components.py
SCREENPIN_OCR_INTEGRATION=1 go test -v ./internal/ocr # Linux；Windows 先设置同名环境变量
```

生成组件位于 `resources/<goos>-<goarch>/workers/`，MyGo 构建会复制到应用旁；发布时保留整个目录，包含模型、运行库和 licenses。构建脚本默认包含离线 OCR，解压后的组件约 350 MiB，实际大小依平台变化。只交叉编译 Go 主程序无法生成完整组件包。CI 在 Windows/Linux 本机编译并上传含组件的产物，另保留三平台 Go 构建检查；CI 配置的增加不等同于本地执行过 Windows 测试。

```bash
go tool mygo build -platform linux/amd64,windows/amd64,darwin/arm64 \
  -skip-installer -skip-dmg -skip-notarize
```

产物位于 `build/native/<platform>/`。开发机也可用 `go build -o build/native/screenpin ./cmd/screenpin` 快速编译；安装时使用 MyGo 构建，保留应用标识和系统集成元数据。

Linux 本地测试可执行 `python scripts/install-local-linux.py`，安装主程序和完整组件到当前用户的 `~/.local`，备份旧版本并重启到长截图窗口。该操作会退出已有 ScreenPin 实例，请先完成正在编辑的图片。

SDK 默认生成的 Linux 安装入口名为 `screenpin.desktop`。ScreenPin 历史门户标识为 `io.screenpin.desktop`，安装入口应命名为 `io.screenpin.desktop.desktop`；下述 Arch 配方保留这个标识。SDK 通用 deb、安装脚本和 PKGBUILD 尚未作为 ScreenPin 发行包验收。

## Arch 原生预览包

在 Arch Linux 仓库根目录执行：

```bash
cd packaging/arch-native
makepkg --syncdeps --cleanbuild
sudo pacman -U ./screenpin-native-0.2.0-1-x86_64.pkg.tar.zst
```

该配方从当前工作区构建，支持 x86_64 和 aarch64，并排除本地凭据、代理状态和编译产物。`screenpin-native` 提供 `screenpin` 命令，与稳定版包互斥。配置仍沿用 `screenpin/config.json`，安装前请从托盘退出旧实例。

## 原生模块

| 目录 | 职责 |
| --- | --- |
| `cmd/screenpin`、`internal/desktop` | 启动参数、单实例、托盘、图片窗口协调、设置、按键说明、自启与退出 |
| `internal/editor` | 单张图片的窗口、画布、输入、工具栏、图片保存与关闭释放 |
| `internal/doc`、`internal/render` | 图片解码、标注模型、内存历史与图像渲染 |
| `internal/capture`、`internal/portal` | 平台截图、私有 D-Bus 连接、截图取消与请求清理 |
| `internal/scrollshot` | 稳定画面判断、拼接、固定栏、暂停和撤回 |
| `internal/ocr`、`internal/components`、`workers` | 离线识别协议、组件定位、平台采集与 OCR 运行时 |
| `internal/shortcutkey`、`internal/shortcuts` | 按键规范化、系统绑定与配置保存事务 |

具体验证记录和后续计划见 [改进记录](../IMPROVEMENTS.md)。

## Release 构建

推送 `v*` 标签运行 `.github/workflows/release-native.yml`，先完成同一提交的全部测试和 Windows、Linux、macOS 两种架构的本机构建，以及 Arch 包构建。所有包内的 OCR 都会在打包后验证。七个附件齐全、版本一致且校验成功后才创建公开 Release；任一构建失败不会发布不完整的版本。

Mac 发行构建使用 macOS 15 运行器，分别生成 arm64/amd64 DMG，执行 ad-hoc 签名并检查签名；未配置开发者证书与公证。Windows 生成 NSIS 安装程序和完整便携包。Linux 安装包保留历史桌面入口 `io.screenpin.desktop.desktop`，便携安装也使用同一门户标识。

目标系统的发行构建先执行 `go tool mygo build -platform <goos>/<goarch> -skip-dmg -skip-notarize`，再执行 `python scripts/package-release.py`。后者会验证版本、组件与真实 OCR，并收集安装包。macOS 还会补齐 MyGo 0.7.4 忽略的 `.dylibs` 目录，重新封装签名，再创建并校验 DMG。Intel macOS 使用仍提供 x86_64 预编译包的 ONNX Runtime 1.23.2，其他平台使用 1.24.4。
