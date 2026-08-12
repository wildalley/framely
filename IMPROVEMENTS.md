# ScreenPin 优化记录

## 改进内容

### 1. 系统托盘图标优化

**改进点：**
- 托盘菜单项显示当前配置的快捷键（例如：截图 (Ctrl+Shift+A)）
- 改进了托盘图标提示文本为 "ScreenPin - 截图标注工具"，更清晰地说明应用功能
- 菜单项文本优化："显示窗口" 替代 "显示 ScreenPin"
- 使用应用默认图标以获得更好的高 DPI 显示效果

**技术实现：**
- `src-tauri/src/lib.rs` 中的 `setup_tray()` 函数
- 从配置中读取快捷键并动态显示在托盘菜单中

### 2. 可配置的全局快捷键

**新功能：**
- 添加了设置面板，用户可以自定义截图的全局快捷键
- 支持组合键录制（Ctrl/Shift/Alt + 字母/数字）
- 快捷键配置持久化保存到用户配置目录
- 动态重新注册快捷键，无需重启应用

**用户界面：**
- 工具栏新增"设置"按钮（齿轮图标）
- 设置面板包含：
  - 快捷键显示输入框（只读，显示当前快捷键）
  - "录制"按钮，点击后可以按下新的快捷键组合
  - 录制状态有动画提示
  - 保存和取消按钮
- 快捷键面板显示当前配置的全局快捷键

**技术实现：**

后端（Rust）：
- 新增 `src-tauri/src/config.rs` 模块，处理配置的加载和保存
- 配置文件路径：`~/.config/screenpin/config.json`（Linux）
- 新增 `get_config` 和 `save_config` 命令
- 配置更改时自动重新注册全局快捷键
- 支持 Wayland 和 X11 环境

前端（TypeScript/React）：
- `src/App.tsx` 新增 `SettingsPanel` 组件
- 快捷键录制逻辑，实时捕获按键组合
- 新增 `AppConfig` 类型定义
- `src/platform.ts` 新增 `getConfig` 和 `saveConfig` 函数

样式（CSS）：
- `src/styles.css` 新增设置面板样式
- 录制按钮的脉冲动画效果
- 响应式布局支持

### 3. 依赖更新

**新增依赖：**
- Rust: `dirs = "5.0"` - 用于跨平台获取配置目录
- Rust: `serde_json = "1"` - JSON 序列化和反序列化

## 配置文件格式

```json
{
  "captureShortcut": "CommandOrControl+Shift+A"
}
```

- `captureShortcut`: 全局快捷键，格式为 Tauri 快捷键字符串
  - `CommandOrControl` 自动映射到 Windows/Linux 的 Ctrl 和 macOS 的 Cmd
  - 支持 `Shift`、`Alt` 修饰键
  - 主键可以是字母（A-Z）、数字（0-9）或特殊键

## 使用说明

### 更改快捷键

1. 打开 ScreenPin 窗口
2. 点击工具栏右侧的"设置"图标（齿轮）
3. 在"截图快捷键"区域点击"录制"按钮
4. 按下你想要的快捷键组合（例如：Ctrl+Shift+S）
5. 点击"保存"按钮

### 查看当前快捷键

- 右键点击系统托盘图标，菜单中会显示当前的快捷键
- 在应用内点击"快捷键 (?)"按钮，第一行显示全局快捷键

## 构建和测试

```bash
# 安装依赖
npm install

# 构建前端
npm run build

# 检查 Rust 代码
cd src-tauri && cargo check

# 运行测试
npm test

# 开发模式运行
npm run tauri dev

# 构建发布版本
npm run tauri build
```

## 兼容性

- ✅ Linux (X11)
- ✅ Linux (Wayland) - 使用桌面门户 API
- ✅ macOS
- ✅ Windows

## 注意事项

1. Wayland 环境下，快捷键通过桌面门户注册，可能需要用户授权
2. 某些系统快捷键可能被占用，建议选择未被使用的组合
3. 配置文件保存在用户配置目录，卸载应用不会删除配置
