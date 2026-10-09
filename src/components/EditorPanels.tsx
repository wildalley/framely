import { useEffect, useState, type KeyboardEvent } from "react";
import {
  ArrowDown,
  ArrowUp,
  Clipboard,
  Download,
  FileArchive,
  FolderOpen,
  ImagePlus,
  Maximize2,
  ScanLine,
  Trash2,
  ZoomIn,
  ZoomOut,
} from "lucide-react";
import { toolName } from "../editor/constants";
import {
  getScreenshotAccess,
  isTauri,
  requestScreenshotAccess,
  type AppConfig,
  type ScreenshotAccess,
} from "../platform";
import {
  COLORS,
  EXPORT_FORMATS,
  isLossyFormat,
  type Annotation,
  type AnnotationStyle,
  type ExportFormat,
  type ImageSource,
} from "../types";

interface StylePanelProps {
  style: AnnotationStyle;
  target: Annotation["type"];
  hasSelection: boolean;
  text: string | null;
  onChange: (change: Partial<AnnotationStyle>, transient?: boolean) => void;
  onText: (text: string) => void;
  onBeginChange: () => void;
  onEndChange: () => void;
  onMoveLayer: (direction: -1 | 1) => void;
  onDelete: () => void;
}

export function StylePanel({
  style,
  target,
  hasSelection,
  text,
  onChange,
  onText,
  onBeginChange,
  onEndChange,
  onMoveLayer,
  onDelete,
}: StylePanelProps) {
  return (
    <div className="pin-panel" role="dialog" aria-label="样式">
      <div className="panel-title">
        <span>{toolName(target)}</span>
        <span className="panel-hint">{hasSelection ? "编辑所选" : "下次绘制"}</span>
      </div>
      {target !== "mosaic" && (
        <div className="color-row">
          {COLORS.map((color) => (
            <button
              key={color}
              className={`color-swatch ${style.color === color ? "is-selected" : ""}`}
              style={{ backgroundColor: color }}
              aria-label={color}
              onClick={() => onChange({ color })}
            />
          ))}
        </div>
      )}
      {target !== "mosaic" && target !== "text" && (
        <Slider
          label="粗细"
          value={style.strokeWidth}
          min={1}
          max={target === "highlighter" ? 40 : 16}
          suffix="px"
          onChange={(strokeWidth) => onChange({ strokeWidth }, true)}
          onBegin={onBeginChange}
          onEnd={onEndChange}
        />
      )}
      {target === "text" && (
        <Slider
          label="字号"
          value={style.fontSize}
          min={12}
          max={96}
          suffix="px"
          onChange={(fontSize) => onChange({ fontSize }, true)}
          onBegin={onBeginChange}
          onEnd={onEndChange}
        />
      )}
      {target !== "mosaic" && (
        <Slider
          label="不透明度"
          value={Math.round(style.opacity * 100)}
          min={10}
          max={100}
          suffix="%"
          onChange={(value) => onChange({ opacity: value / 100 }, true)}
          onBegin={onBeginChange}
          onEnd={onEndChange}
        />
      )}
      {text !== null && (
        <label className="panel-field">
          <span>文字</span>
          <textarea
            value={text}
            rows={2}
            onFocus={onBeginChange}
            onChange={(event) => onText(event.target.value)}
            onBlur={onEndChange}
          />
        </label>
      )}
      {hasSelection && (
        <div className="panel-actions">
          <button title="下移一层" onClick={() => onMoveLayer(-1)}><ArrowDown size={15} /></button>
          <button title="上移一层" onClick={() => onMoveLayer(1)}><ArrowUp size={15} /></button>
          <button className="danger" title="删除 (Delete)" onClick={onDelete}><Trash2 size={15} /></button>
        </div>
      )}
    </div>
  );
}

interface SliderProps {
  label: string;
  value: number;
  min: number;
  max: number;
  suffix: string;
  onChange: (value: number) => void;
  onBegin?: () => void;
  onEnd?: () => void;
}

function Slider({ label, value, min, max, suffix, onChange, onBegin, onEnd }: SliderProps) {
  return (
    <label className="panel-slider">
      <span>{label}</span>
      <output>{value}{suffix}</output>
      <input
        type="range"
        min={min}
        max={max}
        value={value}
        onFocus={onBegin}
        onPointerDown={onBegin}
        onChange={(event) => onChange(Number(event.target.value))}
        onPointerUp={onEnd}
        onPointerCancel={onEnd}
        onBlur={onEnd}
      />
    </label>
  );
}

interface MenuPanelProps {
  title: string;
  image: ImageSource;
  zoom: number;
  busy: boolean;
  onTitle: (title: string) => void;
  onCapture: () => void;
  onPaste: () => void;
  onOpenImage: () => void;
  onOpenProject: () => void;
  onSaveProject: () => void;
  onZoom: (zoom: number) => void;
  onZoomToScreen: () => void;
  exportFormat: ExportFormat;
  exportQuality: number;
  onExportFormat: (format: ExportFormat) => void;
  onExportQuality: (quality: number) => void;
  onSaveImage: () => void;
}

export function MenuPanel({
  title,
  image,
  zoom,
  busy,
  onTitle,
  onCapture,
  onPaste,
  onOpenImage,
  onOpenProject,
  onSaveProject,
  onZoom,
  onZoomToScreen,
  exportFormat,
  exportQuality,
  onExportFormat,
  onExportQuality,
  onSaveImage,
}: MenuPanelProps) {
  return (
    <div className="pin-panel is-menu" role="dialog" aria-label="更多">
      <label className="panel-field">
        <span>名称</span>
        <input aria-label="工程名称" value={title} onChange={(event) => onTitle(event.target.value)} />
      </label>
      <div className="panel-zoom">
        <button title="缩小 (Ctrl+-)" onClick={() => onZoom(zoom - 0.1)}><ZoomOut size={15} /></button>
        <button className="zoom-value" title="100% (Ctrl+0)" onClick={() => onZoom(1)}>{Math.round(zoom * 100)}%</button>
        <button title="放大 (Ctrl++)" onClick={() => onZoom(zoom + 0.1)}><ZoomIn size={15} /></button>
        <button title="适应屏幕" onClick={onZoomToScreen}><Maximize2 size={15} /></button>
      </div>
      <div className="panel-export">
        <span className="panel-export-label">导出格式</span>
        <div className="format-row">
          {EXPORT_FORMATS.map((entry) => (
            <button
              key={entry.id}
              className={`format-button ${exportFormat === entry.id ? "is-selected" : ""}`}
              aria-pressed={exportFormat === entry.id}
              onClick={() => onExportFormat(entry.id)}
            >
              {entry.label}
            </button>
          ))}
        </div>
        {isLossyFormat(exportFormat) && (
          <Slider
            label="质量"
            value={Math.round(exportQuality * 100)}
            min={40}
            max={100}
            suffix="%"
            onChange={(value) => onExportQuality(value / 100)}
          />
        )}
      </div>
      <div className="panel-menu">
        <button onClick={onSaveImage}><Download size={15} />导出 {exportFormat.toUpperCase()} (Ctrl+S)</button>
        <button disabled={busy} onClick={onCapture}><ScanLine size={15} />重新截图</button>
        <button disabled={busy} onClick={onPaste}><Clipboard size={15} />粘贴剪贴板</button>
        <button onClick={onOpenImage}><ImagePlus size={15} />打开图片</button>
        <button onClick={onOpenProject}><FolderOpen size={15} />打开工程</button>
        <button onClick={onSaveProject}><FileArchive size={15} />保存工程 (Ctrl+Shift+S)</button>
      </div>
      <div className="panel-meta">{image.width} × {image.height} · {isTauri() ? "桌面" : "预览"}</div>
    </div>
  );
}

interface ShortcutsPanelProps {
  exportFormat: ExportFormat;
  captureShortcut?: string;
}

export function ShortcutsPanel({ exportFormat, captureShortcut }: ShortcutsPanelProps) {
  const displayShortcut = formatShortcut(captureShortcut || "Super+Shift+S");
  const shortcuts = [
    { keys: displayShortcut.split("+"), action: "截取屏幕（全局）" },
    { keys: ["V"], action: "选择工具" },
    { keys: ["R"], action: "矩形" },
    { keys: ["O"], action: "椭圆" },
    { keys: ["A"], action: "箭头" },
    { keys: ["P"], action: "画笔" },
    { keys: ["H"], action: "荧光笔" },
    { keys: ["G"], action: "区域高亮" },
    { keys: ["T"], action: "文字" },
    { keys: ["M"], action: "马赛克" },
    { keys: ["Ctrl", "Z"], action: "撤销" },
    { keys: ["Ctrl", "Shift", "Z"], action: "重做" },
    { keys: ["Ctrl", "Y"], action: "重做" },
    { keys: ["Ctrl", "C"], action: "复制到剪贴板" },
    { keys: ["Ctrl", "S"], action: `导出 ${exportFormat.toUpperCase()}` },
    { keys: ["Ctrl", "Shift", "S"], action: "保存工程" },
    { keys: ["Ctrl", "+"], action: "放大" },
    { keys: ["Ctrl", "-"], action: "缩小" },
    { keys: ["Ctrl", "0"], action: "重置缩放" },
    { keys: ["Delete"], action: "删除所选" },
    { keys: ["Backspace"], action: "删除所选" },
    { keys: ["Esc"], action: "关闭面板 → 取消选择 → 收回托盘" },
    { keys: ["?"], action: "显示/隐藏快捷键" },
  ];

  return (
    <div className="pin-panel is-shortcuts" role="dialog" aria-label="快捷键">
      <div className="panel-title">
        <span>键盘快捷键</span>
      </div>
      <div className="shortcuts-list">
        {shortcuts.map((shortcut, index) => (
          <div key={index} className="shortcut-item">
            <div className="shortcut-keys">
              {shortcut.keys.map((key, keyIndex) => (
                <span key={keyIndex}>
                  <kbd>{key}</kbd>
                  {keyIndex < shortcut.keys.length - 1 && <span className="shortcut-plus">+</span>}
                </span>
              ))}
            </div>
            <div className="shortcut-action">{shortcut.action}</div>
          </div>
        ))}
      </div>
    </div>
  );
}

interface SettingsPanelProps {
  config: AppConfig;
  /** Read back from the OS, so it can be null while still unknown. */
  autostart: boolean | null;
  onAutostartChange: (enabled: boolean) => void;
  onSave: (config: AppConfig) => void;
  onClose: () => void;
}

export function SettingsPanel({
  config,
  autostart,
  onAutostartChange,
  onSave,
  onClose,
}: SettingsPanelProps) {
  const [shortcut, setShortcut] = useState(config.captureShortcut);
  const [hideAfterCopy, setHideAfterCopy] = useState(config.hideAfterCopy);
  const [isRecording, setIsRecording] = useState(false);

  const handleKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    if (!isRecording) return;
    event.preventDefault();
    event.stopPropagation();

    const parts: string[] = [];
    if (event.metaKey) {
      parts.push(/Mac/i.test(navigator.platform) ? "CommandOrControl" : "Super");
    } else if (event.ctrlKey) {
      parts.push("CommandOrControl");
    }
    if (event.shiftKey) parts.push("Shift");
    if (event.altKey) parts.push("Alt");

    const key = event.key;
    if (key === "Control" || key === "Shift" || key === "Alt" || key === "Meta") return;

    parts.push(key.length === 1 ? key.toUpperCase() : key);
    if (parts.length > 1) {
      setShortcut(parts.join("+"));
      setIsRecording(false);
    }
  };

  return (
    <div className="pin-panel is-settings" role="dialog" aria-label="设置" onKeyDown={handleKeyDown}>
      <div className="panel-title">
        <span>设置</span>
      </div>
      <label className="panel-field">
        <span>截图快捷键</span>
        <div className="shortcut-input-wrapper">
          <input
            className="shortcut-input"
            value={formatShortcut(shortcut)}
            readOnly
            placeholder="点击录制按钮设置快捷键"
          />
          <button className={isRecording ? "is-recording" : ""} onClick={() => setIsRecording(!isRecording)}>
            {isRecording ? "按下快捷键..." : "录制"}
          </button>
        </div>
        <span className="panel-hint">全局快捷键，在任何界面都可触发截图</span>
      </label>
      {isTauri() && (
        <label className="panel-toggle">
          <input
            type="checkbox"
            checked={autostart ?? false}
            disabled={autostart === null}
            onChange={(event) => onAutostartChange(event.target.checked)}
          />
          <span className="panel-toggle-text">
            <span>开机自启</span>
            {/* Autostart lives in the OS, not in config.json, so it takes effect
                immediately instead of waiting for the save button below. */}
            <span className="panel-hint">登录后自动在后台启动，随时用快捷键截图（改动立即生效）</span>
          </span>
        </label>
      )}
      {isTauri() && <ScreenshotAccessRow />}
      <label className="panel-toggle">
        <input
          type="checkbox"
          checked={hideAfterCopy}
          onChange={(event) => setHideAfterCopy(event.target.checked)}
        />
        <span className="panel-toggle-text">
          <span>复制后收回托盘</span>
          <span className="panel-hint">适合截完就贴；窗口置顶时不收回</span>
        </span>
      </label>
      <div className="panel-actions">
        <button onClick={onClose}>取消</button>
        <button className="primary-action" onClick={() => onSave({ captureShortcut: shortcut, hideAfterCopy })}>
          保存
        </button>
      </div>
    </div>
  );
}

const ACCESS_TEXT: Record<Exclude<ScreenshotAccess, "unsupported">, string> = {
  granted: "已授权，截图时直接进入 ScreenPin 选区",
  unknown: "尚未授权，截图会改用系统的选区界面",
  denied: "已被拒绝；可在系统设置的「应用 → ScreenPin」里重新允许截图",
};

/**
 * Silent screenshots need a one-time grant. GNOME only shows that dialog for
 * the focused app, which a shortcut-triggered capture often is not, so offer
 * it here where the click itself focuses the window.
 */
function ScreenshotAccessRow() {
  const [access, setAccess] = useState<ScreenshotAccess | null>(null);
  const [asking, setAsking] = useState(false);

  useEffect(() => {
    void getScreenshotAccess().then(setAccess).catch(() => setAccess("unsupported"));
  }, []);

  if (!access || access === "unsupported") return null;

  const ask = async () => {
    setAsking(true);
    try {
      setAccess(await requestScreenshotAccess());
    } catch {
      setAccess(await getScreenshotAccess().catch(() => access));
    } finally {
      setAsking(false);
    }
  };

  return (
    <div className="panel-field">
      <span>静默截图</span>
      <div className="access-row">
        <span className={`panel-hint access-status is-${access}`}>{ACCESS_TEXT[access]}</span>
        {access === "unknown" && (
          <button disabled={asking} onClick={() => void ask()}>
            {asking ? "等待授权…" : "授权"}
          </button>
        )}
      </div>
    </div>
  );
}

function formatShortcut(shortcut: string) {
  return shortcut
    .replace("CommandOrControl", "Ctrl")
    .replace("Super", "Win");
}
