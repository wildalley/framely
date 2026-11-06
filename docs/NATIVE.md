# MyGo 原生预览版

`refactor/mygo-native` 使用 Go + MyGo 实现托盘截图与贴图工具。启动时只显示托盘；每张图片独立开窗，关闭就销毁图片、标注和撤销记录。没有主页、截图历史或工程保存流程。稳定发行版仍为 Tauri `v0.1.1`。

## 使用

从托盘选择「截图」「粘贴图片」或「打开图片」。已有图片继续保留，新图片在另一个窗口打开，可以同时置顶多张图片。截图期间暂时隐藏 ScreenPin 的窗口，结束后恢复仍然打开的窗口。

图片窗口只有图片和紧凑工具栏：八种标注、样式、撤销/重做、置顶、保存和复制。MyGo 工具栏自动把放不下的标注工具收入菜单，复制、保存、置顶和关闭保持可见；工具栏支持 Tab 和方向键，画布右键使用原生菜单。控件跟随系统明暗主题，标注支持移动、缩放、样式、图层以及文字原位编辑。

- `Ctrl/Cmd+C` 复制图片，`Ctrl/Cmd+S` 保存 PNG 或 JPEG；保存菜单可切换格式和 JPEG 质量。
- `Ctrl/Cmd+V` 粘贴到新的图片窗口，`Ctrl/Cmd+W` 直接关闭并销毁当前图片。
- `Esc` 依次关闭样式、取消选中并返回选择工具、关闭图片。关闭不询问未保存修改。
- 从托盘打开「快捷键与设置」或「查看按键」，不需要先截图。工具窗口关闭后也会销毁。
- 设置「复制后关闭图片」适合截完即贴；置顶图片继续保留。沿用既有 JSON 字段 `hideAfterCopy`，不会保留隐藏图片。
- 开机自启由系统管理，登录后在托盘待命；没有托盘时显示设置窗口，关闭最后一个窗口会退出，避免留下无法操作的后台实例。

支持导入 PNG / JPEG / WebP，保存 PNG / JPEG。原生版不打开或写入 `.screenpin` 工程；如需处理旧工程，请先用 Tauri 稳定版导出图片。预览包不注册工程或图片文件关联。

Linux 首次截图使用系统选区；可从设置中点击「允许直接选区」授权。已获静默截图权限时使用 ScreenPin 自己的选区，拖动后松开截取、拖动时按住空格移动、Enter 截取整块显示器、Esc 或右键取消。权限或自有选区不可用时使用系统选区，取消不会创建空窗口。退出会取消正在等待的截图和选区。

全局截图默认 `Win+Shift+S`，可从托盘设置录制其他组合键。Wayland 下等待系统实际授权结果，并显示实际分配的按键；新注册和配置保存都成功后才替换旧绑定，失败时保留旧绑定。关闭等待中的设置窗口会取消候选请求。也可以在桌面快捷键设置中绑定 `screenpin --capture`。

WebP 保存尚未迁移。Windows/macOS 交互、真实 Wayland 授权与多显示器选区仍需桌面验收；跨平台构建和独立 D-Bus 协议测试不能代替这些检查。

## 开发与检查

需要 Go 1.27.3 或更新版本。Linux 原生窗口需要 GTK3、DBus、AppIndicator 和 `xdg-desktop-portal`，不需要 WebKitGTK。Wayland 需要适合桌面环境的 portal 后端。

```bash
go run ./cmd/screenpin
go run ./cmd/screenpin --capture
go run ./cmd/screenpin ./example.png
go test -race ./...
go vet ./...
```

默认启动和再次启动都只让托盘待命；`--hidden` 保留为兼容参数。`--capture` 和图片参数交给已有实例，每次打开独立图片，不替换当前图片。

Linux 门户测试使用独立会话总线，不调用当前桌面的门户。MyGo 无窗口控件回归覆盖文字输入焦点、工具栏溢出与方向键、画布菜单、关闭时的状态释放及异步结果隔离。CI 执行这些检查并构建 Linux、Windows 和 macOS 原生产物。

## 构建

```bash
go tool mygo build -platform linux/amd64,windows/amd64,darwin/arm64 \
  -skip-installer -skip-dmg -skip-notarize
```

产物位于 `build/native/<platform>/`。开发机也可用 `go build -o build/native/screenpin ./cmd/screenpin` 快速编译；安装时使用 MyGo 构建，保留应用标识和系统集成元数据。

SDK 默认生成的 Linux 安装入口名为 `screenpin.desktop`。ScreenPin 历史门户标识为 `io.screenpin.desktop`，安装入口应命名为 `io.screenpin.desktop.desktop`；下述 Arch 配方保留这个标识。SDK 通用 deb、安装脚本和 PKGBUILD 尚未作为 ScreenPin 发行包验收。

## Arch 原生预览包

在 Arch Linux 仓库根目录执行：

```bash
cd packaging/arch-native
makepkg --syncdeps --cleanbuild
sudo pacman -U ./screenpin-native-0.1.1-2-x86_64.pkg.tar.zst
```

该配方从当前工作区构建，支持 x86_64 和 aarch64，并排除本地凭据、代理状态和编译产物。`screenpin-native` 提供 `screenpin` 命令，与稳定版包互斥。配置仍沿用 `screenpin/config.json`，安装前请从托盘退出旧实例。

## 原生模块

| 目录 | 职责 |
| --- | --- |
| `cmd/screenpin`、`internal/desktop` | 启动参数、单实例、托盘、图片窗口协调、设置、按键说明、自启与退出 |
| `internal/editor` | 单张图片的窗口、画布、输入、工具栏、图片保存与关闭释放 |
| `internal/doc`、`internal/render` | 图片解码、标注模型、内存历史与图像渲染 |
| `internal/capture`、`internal/portal` | 平台截图、私有 D-Bus 连接、截图取消与请求清理 |
| `internal/shortcutkey`、`internal/shortcuts` | 按键规范化、系统绑定与配置保存事务 |

具体验证记录和后续计划见 [改进记录](../IMPROVEMENTS.md)。
