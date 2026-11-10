# ScreenPin

ScreenPin 是一个面向 Linux、Windows 和 macOS 的轻量、现代的桌面截图、标注与贴图工具。原生版使用 **Go + MyGo** 重制，采用托盘常驻与独立多贴图窗口架构：每次截图、粘贴或打开图片都会创建独立贴图窗口，支持标注涂鸦、置顶、复制、导出与离线 OCR；关闭贴图后即刻释放内存与图片资源。

版本变更详见 [更新日志](CHANGELOG.md)，开发演进与测试见 [原生开发说明](docs/NATIVE.md) 与 [改进记录与计划](IMPROVEMENTS.md)。

---

## 核心特性

- **多平台原生截图与快速选区**
  - **Linux (Wayland / X11)**：通过 `xdg-desktop-portal` + PipeWire 原生采集，各显示器分别记忆授权；支持全屏选区拖选、按住空格移动选区、`Enter` 全屏、`Esc` 取消。无需依赖 WebKitGTK。
  - **Windows**：纯内存桌面采集与自有原生全屏选区，点击保存前不产生磁盘临时文件。
  - **macOS**：原生桌面捕获与窗口展示。
- **现代化极简工具栏与快捷属性栏**
  - 极简无框底图标与强调色激活状态，按功能逻辑分组。
  - 现代连体胶囊数值组件 `[ 标签 数值 | ▲▼ ]`，支持直接输入或快捷加减。
  - 标注线宽、字体大小、矩形及整图圆角、不透明度、颜色调色板一键直达。
- **智能长截图（滚动截图）**
  - 进入长截图后直接框选、松开即开始；Windows 10 2004+ 与 Linux 提供原生浮动控制条。
  - 边滚边拼（40ms 极低延迟持续采样），无需等待拖动停止；自动识别去除右侧灰色滚动条，固定页脚防重复追加。
- **离线中英文 OCR 文字识别**
  - 开箱即用内置本地离线 OCR 模型与独立子进程运行时，不依赖系统安装 Python。
  - 纯本地推理，支持框选识别、长图逐块返回、全文编辑，以及类似「大爆炸」的中文分词与选词组合复制。
  - 托盘「框选取字…」或 `screenpin --ocr` 截图后直接进入结果；支持点选、Shift 连选、全选/反选和单字模式。
  - 连续识别复用模型，空闲 45 秒后释放子进程。识别与分词均不调用云端 API。
- **独立多贴图与高效内存释放**
  - 支持快捷键连续截图，每张贴图独立开窗，可随意移动、缩放、置顶（Pin）于屏幕最前。
  - 贴图关闭后即刻销毁图片对象与撤销历史，严格控制驻留内存（日常待机仅约 20~30 MB）。
  - 托盘图标动态显示全局快捷键与当前贴图数，支持「关闭所有贴图」一键批量清理。
- **多样导出**
  - PNG、WebP、JPEG 多格式导出；WebP / JPEG 质量无级可调；支持保留透明四角（PNG/WebP）或智能铺白底（JPEG）。

---

## 下载与安装

安装包请从 [Releases](https://github.com/wildalley/screenpin/releases) 获取；当前原生源码版本为 `0.2.2`，本文中的开发改动以实际发布说明为准。完整安装包均内置离线中英文 OCR 组件。

| 系统平台 | 安装方式 / 文件 |
| :--- | :--- |
| **Windows x64** | `screenpin-0.2.2-windows-amd64-setup.exe`；另提供免安装 `screenpin-0.2.2-windows-amd64-portable.zip` |
| **macOS 15+ (Apple Silicon)** | `screenpin-0.2.2-darwin-arm64.dmg`（下载后将 ScreenPin 拖入 Applications） |
| **macOS 15+ (Intel)** | `screenpin-0.2.2-darwin-amd64.dmg` |
| **Ubuntu / Debian x64** | `sudo apt install ./screenpin-0.2.2-linux-amd64.deb` |
| **Arch Linux x64** | `sudo pacman -U ./screenpin-native-0.2.2-1-x86_64.pkg.tar.zst` |
| **其他 Linux x64** | 解压 `screenpin-0.2.2-linux-amd64-portable.tar.gz` 后运行 `./install.sh` 或 `./screenpin` |

所有安装包的 SHA-256 校验和见 Release 中的 `SHA256SUMS` 文件。

> [!NOTE]
> 平台说明与发布细节请参阅 [v0.2.2 发布说明](docs/releases/v0.2.2.md)。

---

## 本地开发与构建

### 依赖环境
- **Go**：>= 1.27.3（与 `go.mod` 一致）
- **MyGo 工具链**：`mygo`
- **Linux 编译依赖**：`libayatana-appindicator3-dev`、`libpipewire-0.3-dev`、`pkg-config`

### 常用命令

```bash
# 运行单元测试
go test ./...

# 运行代码竞态检测
go test -race ./...

# 构建本地平台原生二进制与安装包
go tool mygo build --platform linux/amd64

# 一键安装到当前用户本地目录 (~/.local/bin) 并启动
python3 scripts/install-local-linux.py --image assets/app-icon.png
```

详细的各平台编译步骤、离线 OCR 构建流程及测试见 [原生开发说明](docs/NATIVE.md)。

---

## 快捷键与配置

可以在设置面板中直接录制全局截图快捷键。配置文件路径位于操作系统标准配置目录：
- **Linux**：`~/.config/screenpin/config.json`
- **Windows**：`%APPDATA%\ScreenPin\config.json`

默认配置：
```json
{
  "captureShortcut": "Ctrl+Alt+A",
  "hideAfterCopy": false
}
```

- Linux Wayland 下保存快捷键会自动请求桌面门户（`org.freedesktop.portal.GlobalShortcuts`）授权。
- 开机自启由操作系统注册表 / 自启目录管理，在托盘菜单或设置中即可一键开关。

---

## 常用操作指引

- **框选截图**：默认快捷键 `Ctrl + Alt + A`（支持自定义），拖拽选择范围，松开即可生成贴图窗口。
- **滚动长截图**：托盘菜单选择「长截图」或按长截图快捷键，框选要滚动的区域，松开后滚动鼠标滚轮或拖动内容，再次按快捷键或点击浮动栏「完成」即可生成长图。
- **快速固定与置顶**：点击工具栏图钉或双击标题栏，窗口将固定在所有应用最前层。
- **标注编辑**：画笔、矩形、圆形、箭头、荧光笔、文字、马赛克。画完图形后可直接拖拽四角调节点缩放或移动。
- **复制与保存**：`Ctrl + C` 一键复制合成图，`Ctrl + S` 弹出保存选项。

---

## License

[MIT](LICENSE)
