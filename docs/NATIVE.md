# MyGo 原生预览版

`refactor/mygo-native` 将编辑器和桌面入口迁移到 Go + MyGo，当前已接通可运行的原生应用。稳定发行版仍是 Tauri `v0.1.1`；两者共用 `.screenpin` 工程格式和 `screenpin/config.json` 配置。

## 已接通的功能

- 八种可编辑标注、移动和缩放、样式与图层、撤销/重做、文字输入焦点隔离。
- PNG / JPEG 导出、剪贴板图片复制和 `Ctrl/Cmd+V` 粘贴、工程保存与打开；可导入 PNG / JPEG / WebP。
- 原生窗口、托盘、置顶、系统自启、未保存退出确认；托盘不可用时关闭窗口会退出。
- `--hidden`、`--capture`、单实例转交，以及图片或工程文件启动参数。
- Linux 截图门户与选区窗口、Windows 系统截图、macOS `screencapture` 接口。
- Wayland 快捷键等待实际授权结果，展示门户分配的按键；新绑定和配置保存成功后才替换旧绑定。取消或失败会清理候选请求和会话，保留原绑定。

WebP 导出尚未迁移。Windows/macOS 交互、真实 Wayland 授权与多显示器选区仍需桌面验收；跨平台构建和独立 D-Bus 协议测试不能代替这些检查。

## 开发与检查

需要 Go 1.27.3 或更新版本。Linux 原生窗口需要 GTK3、DBus、AppIndicator 和 `xdg-desktop-portal`，不需要 WebKitGTK。Wayland 还需要适合桌面环境的 portal 后端；后端不支持全局快捷键时，可在桌面设置中绑定 `screenpin --capture`。

```bash
go run ./cmd/screenpin
go run ./cmd/screenpin --hidden
go run ./cmd/screenpin ./example.screenpin
go test -race ./...
go vet ./...
```

Linux 的门户集成测试需要 `dbus-daemon`，每项测试启动独立会话总线，不调用当前桌面的门户。文字输入回归使用 MyGo 的无窗口测试器。CI 执行这些检查，并构建 Linux、Windows 和 macOS 原生预览产物。

## 构建

```bash
go tool mygo build -platform linux/amd64,windows/amd64,darwin/arm64 \
  -skip-installer -skip-dmg -skip-notarize
```

产物位于 `build/native/<platform>/`。开发机也可用 `go build -o build/native/screenpin ./cmd/screenpin` 快速编译；安装时使用 MyGo 构建，保留应用标识和系统集成元数据。

SDK 默认生成的 Linux 安装入口名为 `screenpin.desktop`。ScreenPin 的历史门户标识为 `io.screenpin.desktop`，安装入口需要命名为 `io.screenpin.desktop.desktop`；当前使用下述独立 Arch 配方保留这个标识。SDK 附带的通用 deb、安装脚本和 PKGBUILD 尚未作为 ScreenPin 发行包验收。

## Arch 原生预览包

在 Arch Linux 仓库根目录执行：

```bash
cd packaging/arch-native
makepkg --syncdeps --cleanbuild
sudo pacman -U ./screenpin-native-0.1.1-1-x86_64.pkg.tar.zst
```

该配方从当前工作区构建，支持 x86_64 和 aarch64，并排除本地凭据、代理状态和编译产物。`screenpin-native` 提供 `screenpin` 命令，与稳定版 `screenpin` 包互斥。预览版沿用既有配置和工程，安装前请从托盘退出旧实例。

## 原生模块

| 目录 | 职责 |
| --- | --- |
| `cmd/screenpin`、`internal/desktop` | 启动参数、单实例、托盘、自启和退出生命周期 |
| `internal/editor` | 窗口、画布、输入焦点、面板和文件操作 |
| `internal/doc`、`internal/render` | 工程兼容、标注历史与原生图像导出 |
| `internal/capture`、`internal/portal` | 平台截图、私有 D-Bus 连接和门户请求清理 |
| `internal/shortcutkey`、`internal/shortcuts` | 按键规范化、门户绑定和配置保存事务 |

具体验证记录和后续迁移计划见 [改进记录](../IMPROVEMENTS.md)。
