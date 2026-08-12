# ScreenPin

ScreenPin 是一个面向桌面的非破坏性截图编辑器，目标平台为 Linux、Windows 和 macOS。它借鉴 PixPin 的”截图后继续工作”思路，但把标注保留为可编辑对象：工程文件保存原图、对象坐标、样式和图层顺序，PNG 只是导出格式。

## 特性

- **跨平台截图采集**
  - Linux Wayland/X11：通过 `xdg-desktop-portal` 交互式截图，选择区域由系统门户处理
  - macOS：调用 `screencapture -i` 选择区域
  - Windows：优先打开系统 `ms-screenclip:` 选择区域，超时后回退到虚拟桌面截图
  
- **八种标注工具**：矩形、椭圆、箭头、画笔、荧光笔、区域高亮、文字、马赛克
  - 区域高亮（`G`）：拖出一个或多个选区，选区内保持原图亮度，其余区域压暗，适合聚焦讲解重点
  
- **实时编辑**
  - 选中对象后移动、四角缩放、改颜色、调粗细/透明度、调整图层和删除
  - 文字对象双击直接编辑，新建文字自动进入编辑状态
  - `Ctrl/Cmd + Z`、`Ctrl/Cmd + Shift + Z` 撤销/重做
  - 按 `?` 打开快捷键面板，列出全部工具和操作按键
  - 无边框窗口，动态适配截图尺寸和缩放比例
  - 大图低缩放时，选框控制点保持可操作尺寸
  
- **工作流集成**
  - 复制到剪贴板，导出 PNG / JPEG / WebP（有损格式可调质量），保存/打开 `.screenpin` 工程
  - 马赛克处理失败时不会回退到原图，相关工具会被禁用，含马赛克的导出也会被阻止
  - 置顶窗口继续修改，再次点击取消置顶
  - 桌面级 `Ctrl/Cmd + Shift + A` 全局快捷键
  - 系统托盘菜单：截图、显示主窗口、退出；关闭主窗口隐藏到托盘

## 开发

```bash
npm install
npm run dev
```

桌面端：

```bash
npm run tauri -- dev
```

在 Wayland 会话中，ScreenPin 检测到 XWayland 后会自动让 WebKitGTK 窗口使用 X11，并关闭 DMA-BUF 渲染，以规避部分合成器上的 GDK protocol error 71。截图本身仍通过 `xdg-desktop-portal` 请求，不会绕过 Wayland 权限模型。

如需测试原生 Wayland 窗口后端，可以显式覆盖：

```bash
SCREENPIN_GDK_BACKEND=wayland npm run tauri -- dev
```

检查：

```bash
npm run build
npm test
cd src-tauri && cargo check && cargo test
```

### Linux 依赖

需要 WebKitGTK、GTK3、DBus 和 `xdg-desktop-portal`。Wayland 下截图权限与交互界面由桌面环境的 portal 后端决定；Hyprland 建议安装并启用 `xdg-desktop-portal-hyprland`。Wayland 全局快捷键使用 `org.freedesktop.portal.GlobalShortcuts`，首次启动可能由桌面门户请求授权。托盘图标使用 AppIndicator/StatusNotifier，Waybar 需要启用 `tray` 模块才能显示。

### 平铺窗口管理器

Hyprland、Sway 等平铺合成器会接管窗口尺寸，`fit_window` 的调整可能被忽略，窗口也会被塞进 tiling 布局。建议加窗口规则让它浮动，例如 Hyprland 的 `~/.config/hypr/hyprland.conf`：

```
windowrulev2 = float, class:^(ScreenPin)$
windowrulev2 = noborder, class:^(ScreenPin)$
windowrulev2 = noanim, class:^(ScreenPin)$
```

类名以实际 `hyprctl clients` 输出为准。

## 工程格式

`.screenpin` 是版本化 JSON：

```json
{
  "format": "screenpin-project",
  "version": 1,
  "title": "产品截图",
  "createdAt": "2026-08-11T07:30:00.000Z",
  "updatedAt": "2026-08-11T08:15:00.000Z",
  "image": { "dataUrl": "...", "width": 1920, "height": 1080, "name": "screenshot.png" },
  "annotations": [
    { "id": "...", "type": "arrow", "x": 120, "y": 80, "endX": 420, "endY": 250, "color": "#e5484d", "strokeWidth": 4, "opacity": 1 }
  ]
}
```

区域高亮对象使用 `type: "area-highlight"`，并保存选区的 `x`、`y`、`width`、`height`、颜色和压暗强度。工程打开时会校验图片尺寸、时间字段、标注类型、数值范围和对象 ID，非法或损坏的工程会被拒绝加载。

后续版本可以在保持原图和对象坐标兼容的前提下增加 OCR、滚动截图、窗口录制、云同步和多图画布。

## 技术栈

- **前端**：React 19 + TypeScript + Vite
- **桌面**：Tauri 2 (Rust)
- **测试**：Vitest
- **平台集成**：
  - Linux：`xdg-desktop-portal` (截图)、`ashpd` (全局快捷键)、`arboard` (剪贴板)
  - macOS：`screencapture` CLI
  - Windows：`ms-screenclip:` URI + Windows API fallback

## 架构

无边框窗口通过 `fit_window` command 动态调整尺寸以匹配截图缩放比例 + 工具栏高度。工具栏使用 ResizeObserver 测量自身，通过 CSS 自定义属性 `--bar-height` 传递给浮层定位。缩放计算受屏幕工作区约束，保证窗口不会超出可用空间。

标注对象是 SVG 元素，保留原始坐标和样式参数。区域高亮通过 SVG mask 对全画布添加暗层，并挖出选区，因此导出图像中选区保持原图亮度、其他区域变暗。导出时克隆 SVG、移除选择框和编辑器元素、序列化为 Blob 后通过 Canvas 渲染，再按所选格式编码。马赛克使用低分辨率纹理并以像素化方式放大；如果纹理生成失败，应用会拒绝导出以避免泄露原图。JPEG 没有 alpha 通道，编码前先铺白底避免透明像素变黑；浏览器遇到不支持的格式会静默回退成 PNG，这种情况会直接报错而不是写出扩展名与实际内容不符的文件。

## License

MIT
