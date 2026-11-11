# Framely 图标

2026-11-11 起使用“折叠画面 F”：像一张被取下、折起并贴住的画面，外轮廓也对应 Framely 的首字母。它替代四角取景框与中心圆点，避免与常见截图工具图标混淆。

应用图标使用深青 `#073D41`、暖白 `#FFF9ED` / `#F3E5CF` 和折角橙 `#ED782F`。透明区域仅在圆角底板外。图形不依赖字标，32px 下仍保留主要轮廓。

| 资产 | 用途 |
| --- | --- |
| `assets/app-icon.svg` | 应用图标 SVG 母版 |
| `assets/app-icon.png`、`assets/icons/*` | 原生打包和 Linux 桌面菜单，多尺寸 PNG |
| `assets/mark.svg` | 界面标题中的 `currentColor` 剪影 |
| `assets/tray-icon.svg` | Linux 托盘，高对比青绿色剪影 |
| `assets/tray-template.svg` | macOS/Windows 的黑色模板剪影 |
| `assets/tray-recording.svg` | 录制中图标，增加红色角标 |

安装 librsvg 后，在仓库根目录运行 `python3 scripts/render-icons.py`，即可从 SVG 同步生成全部 PNG。不要单独改某个尺寸的 PNG。

## 设计来源

先使用 Codex 内置 imagegen 生成视觉概念，再手工整理成可编辑 SVG，简化曲线、折面和阴影，独立绘制 16–32px 用的剪影。没有采用其他软件的图标或截图。

本轮生成概念保存在工作区 `build/brand/folded-f-concept.png`（构建目录，不随源码提交）。生产资产以 SVG 母版为准。

生成提示词：

> Use case: logo-brand. Create one distinctive production-ready desktop app icon for Framely (a native screenshot, pinning and screen recording tool). This is a NEW brand concept, not a modification. Single centered icon, square composition, no text or letters around it, no grid or variants. Dark deep petrol teal rounded-square tile. Inside: a sculptural yet very simple warm ivory folded paper ribbon, an asymmetric angular F-like abstract silhouette made of two layered image-paper tabs, with one small burnt-orange folded corner. Think a captured piece of the screen carefully folded and held, not a camera. Bold graphic silhouette legible at 24 pixels; very few geometric planes, generous negative space, sharp intentional folds with subtly rounded exterior edges. Premium quiet editorial identity, restrained flat vector-like color blocking, tiny tactile depth at the overlap only. No crop brackets, no viewfinder, no center dot, no lens, no camera outline, no generic pin, no sparkle, no letterpress lettering, no glass gradients, no glossy 3D, no watermark. Transparent pixels outside the rounded-square tile.

## 展示截图

`docs/images/framely.png` 由真实编辑器视图的软件渲染器生成，画布为 `docs/images/demo-canvas.svg` 的生成示例内容，带一处选中的真实矩形标注和浮动样式条；不是用户的桌面或文件截图，也不是 imagegen 生成的应用界面。

复现：

```sh
mkdir -p build/brand/previews
rsvg-convert docs/images/demo-canvas.svg -o build/brand/demo-canvas.png
SCREENPIN_UI_SCREENSHOTS="$PWD/build/brand/previews" \
SCREENPIN_PREVIEW_IMAGE="$PWD/build/brand/demo-canvas.png" \
go test ./internal/editor -run TestImageWindowPreview -count=1
cp build/brand/previews/editor-1000-dark-false-style.png docs/images/framely.png
```
