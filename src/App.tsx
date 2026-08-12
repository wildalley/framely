import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import {
  Aperture,
  ArrowDown,
  ArrowUp,
  Check,
  Circle,
  Clipboard,
  Copy,
  Download,
  FileArchive,
  FolderOpen,
  Grid3X3,
  HelpCircle,
  Highlighter,
  ImagePlus,
  Maximize2,
  MoreHorizontal,
  MousePointer2,
  MoveUpRight,
  Palette,
  Pencil,
  Pin,
  PinOff,
  Redo2,
  ScanLine,
  Square,
  Trash2,
  Type,
  Undo2,
  X,
  ZoomIn,
  ZoomOut,
} from "lucide-react";
import type { LucideIcon } from "lucide-react";
import { listen } from "@tauri-apps/api/event";
import { EditorCanvas, type EditorCanvasHandle } from "./EditorCanvas";
import {
  captureScreen,
  copyImage,
  createDemoImage,
  fitWindow,
  hideWindow,
  imageFromFile,
  isTauri,
  openProject,
  pasteImage,
  pixelateImage,
  saveImageAs,
  saveProject,
  setMainPinned,
  startWindowDrag,
  workArea,
} from "./platform";
import type {
  Annotation,
  AnnotationStyle,
  CaptureResult,
  ExportFormat,
  ImageSource,
  ScreenPinProject,
  Tool,
} from "./types";
import { COLORS, EXPORT_FORMATS, defaultStyle, isLossyFormat, isScreenPinProject } from "./types";
import { useEditorHistory } from "./useEditorHistory";

const TOOL_ITEMS: { id: Tool; label: string; icon: LucideIcon; key: string }[] = [
  { id: "select", label: "选择", icon: MousePointer2, key: "V" },
  { id: "rectangle", label: "矩形", icon: Square, key: "R" },
  { id: "ellipse", label: "椭圆", icon: Circle, key: "O" },
  { id: "arrow", label: "箭头", icon: MoveUpRight, key: "A" },
  { id: "pen", label: "画笔", icon: Pencil, key: "P" },
  { id: "highlighter", label: "荧光笔", icon: Highlighter, key: "H" },
  { id: "text", label: "文字", icon: Type, key: "T" },
  { id: "mosaic", label: "马赛克", icon: Grid3X3, key: "M" },
];

const DRAW_TOOLS = TOOL_ITEMS.map((item) => item.id).filter(
  (id): id is Annotation["type"] => id !== "select",
);

const MIN_ZOOM = 0.1;
const MAX_ZOOM = 3;
/** Enough room for the toolbar to stay usable next to very small screenshots. */
const MIN_WINDOW_WIDTH = 320;
const EMPTY_WINDOW = { width: 460, height: 300 };
const ASSUMED_BAR_HEIGHT = 46;

const clamp = (value: number, min: number, max: number) => Math.min(max, Math.max(min, value));
const roundZoom = (value: number) => Math.round(value * 100) / 100;

/** Largest zoom that keeps image plus toolbar inside the usable desktop area. */
function zoomToFit(image: ImageSource, area: { width: number; height: number } | null, barHeight: number) {
  if (!area) return 1;
  const usableWidth = Math.max(120, area.width - 24);
  const usableHeight = Math.max(120, area.height - barHeight - 24);
  return roundZoom(clamp(Math.min(usableWidth / image.width, usableHeight / image.height, 1), MIN_ZOOM, 1));
}
export default function App() {
  const canvasRef = useRef<EditorCanvasHandle>(null);
  const stageRef = useRef<HTMLDivElement>(null);
  const barRef = useRef<HTMLDivElement>(null);
  const imageInput = useRef<HTMLInputElement>(null);
  const projectInput = useRef<HTMLInputElement>(null);
  const demoCapture = useMemo(() => (new URLSearchParams(window.location.search).has("demo") ? createDemoImage() : null), []);
  const [image, setImage] = useState<ImageSource | null>(() => demoCapture ? { ...demoCapture, name: "交付概览" } : null);
  const [pixelatedImage, setPixelatedImage] = useState("");
  const [title, setTitle] = useState(demoCapture ? "交付概览" : "未命名截图");
  const [createdAt, setCreatedAt] = useState(() => new Date().toISOString());
  const [tool, setTool] = useState<Tool>("select");
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [zoom, setZoom] = useState(1);
  const [isPinned, setIsPinned] = useState(false);
  const [busy, setBusy] = useState(false);
  const [toast, setToast] = useState<string | null>(null);
  const [panel, setPanel] = useState<"none" | "style" | "menu" | "shortcuts">("none");
  const [styles, setStyles] = useState<Record<Annotation["type"], AnnotationStyle>>(() =>
    DRAW_TOOLS.reduce(
      (all, id) => ({ ...all, [id]: defaultStyle(id) }),
      {} as Record<Annotation["type"], AnnotationStyle>,
    ),
  );
  const [exportFormat, setExportFormat] = useState<ExportFormat>("png");
  const [exportQuality, setExportQuality] = useState(0.92);
  const [barHeight, setBarHeight] = useState(ASSUMED_BAR_HEIGHT);
  const area = useRef<{ width: number; height: number } | null>(null);
  const barHeightRef = useRef(barHeight);
  const recenter = useRef(true);
  const history = useEditorHistory();

  const selected = useMemo(
    () => history.annotations.find((annotation) => annotation.id === selectedId) ?? null,
    [history.annotations, selectedId],
  );

  const notify = useCallback((message: string) => {
    setToast(message);
    window.setTimeout(() => setToast((current) => (current === message ? null : current)), 2400);
  }, []);

  const refreshArea = useCallback(async () => {
    if (!isTauri()) {
      area.current = { width: window.innerWidth, height: window.innerHeight };
      return area.current;
    }
    try {
      area.current = await workArea();
    } catch {
      area.current = { width: window.screen.availWidth, height: window.screen.availHeight };
    }
    return area.current;
  }, []);

  useEffect(() => {
    void refreshArea();
  }, [refreshArea]);
  const loadCapture = useCallback(
    async (capture: CaptureResult, name = `截图 ${new Date().toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit" })}`) => {
      const source = { ...capture, name };
      const fitted = zoomToFit(source, await refreshArea(), barHeightRef.current);
      setImage(source);
      setTitle(name.replace(/\.[^.]+$/, ""));
      setCreatedAt(new Date().toISOString());
      setSelectedId(null);
      setTool("select");
      setPanel("none");
      history.reset();
      setZoom(fitted);
      recenter.current = true;
      try {
        setPixelatedImage(await pixelateImage(capture.dataUrl, capture.width, capture.height));
      } catch {
        setPixelatedImage(capture.dataUrl);
      }
    },
    [history.reset, refreshArea],
  );

  useEffect(() => {
    if (demoCapture) loadCapture(demoCapture, "交付概览");
  }, [demoCapture, loadCapture]);

  // Keep the frameless window hugging the stage and the toolbar.
  const stage = image
    ? { width: Math.round(image.width * zoom), height: Math.round(image.height * zoom) }
    : null;

  useLayoutEffect(() => {
    barHeightRef.current = barHeight;
    if (!isTauri()) return;
    const center = recenter.current;
    recenter.current = false;
    const size = stage
      ? { width: Math.max(MIN_WINDOW_WIDTH, stage.width), height: stage.height + barHeight }
      : EMPTY_WINDOW;
    void fitWindow(size.width, size.height, center).catch(() => undefined);
  }, [stage?.width, stage?.height, barHeight]);

  useEffect(() => {
    const bar = barRef.current;
    if (!bar) return;
    const observer = new ResizeObserver(() => {
      const next = Math.round(bar.getBoundingClientRect().height);
      if (next > 0) setBarHeight((current) => (current === next ? current : next));
    });
    observer.observe(bar);
    return () => observer.disconnect();
  }, [image]);
  const applyZoom = useCallback((next: number) => setZoom(roundZoom(clamp(next, MIN_ZOOM, MAX_ZOOM))), []);

  const zoomToScreen = useCallback(async () => {
    if (image) applyZoom(zoomToFit(image, await refreshArea(), barHeightRef.current));
  }, [applyZoom, image, refreshArea]);

  // Ctrl + wheel zoom needs a non-passive listener to cancel the scroll.
  useEffect(() => {
    const stageNode = stageRef.current;
    if (!stageNode) return;
    const onWheel = (event: WheelEvent) => {
      if (!event.ctrlKey && !event.metaKey) return;
      event.preventDefault();
      setZoom((current) => roundZoom(clamp(current * (event.deltaY < 0 ? 1.1 : 1 / 1.1), MIN_ZOOM, MAX_ZOOM)));
    };
    stageNode.addEventListener("wheel", onWheel, { passive: false });
    return () => stageNode.removeEventListener("wheel", onWheel);
  }, [image]);

  const handleCapture = useCallback(async () => {
    setPanel("none");
    if (!isTauri()) {
      imageInput.current?.click();
      return;
    }
    setBusy(true);
    try {
      await loadCapture(await captureScreen());
    } catch (error) {
      notify(errorMessage(error));
    } finally {
      setBusy(false);
    }
  }, [loadCapture, notify]);

  useEffect(() => {
    if (!isTauri()) return;
    const unlisten = listen("capture-requested", handleCapture);
    return () => {
      void unlisten.then((dispose) => dispose());
    };
  }, [handleCapture]);

  const handlePaste = useCallback(async () => {
    setPanel("none");
    setBusy(true);
    try {
      if (isTauri()) {
        await loadCapture(await pasteImage(), "剪贴板图片");
      } else {
        const items = await navigator.clipboard.read();
        const type = items.flatMap((item) => item.types).find((item) => item.startsWith("image/"));
        if (!type) throw new Error("剪贴板中没有图片");
        const owner = items.find((item) => item.types.includes(type));
        await loadCapture(await imageFromFile(new File([await owner!.getType(type)], "clipboard.png", { type })), "剪贴板图片");
      }
    } catch (error) {
      notify(errorMessage(error));
    } finally {
      setBusy(false);
    }
  }, [loadCapture, notify]);
  const exportImage = useCallback(
    async (format: ExportFormat = "png", quality = 1) => {
      if (!canvasRef.current || !image) throw new Error("还没有可导出的截图");
      return canvasRef.current.exportImage(format, quality);
    },
    [image],
  );

  const handleCopy = useCallback(async () => {
    try {
      const dataUrl = await exportImage();
      if (isTauri()) {
        await copyImage(dataUrl);
      } else {
        const blob = await (await fetch(dataUrl)).blob();
        await navigator.clipboard.write([new ClipboardItem({ "image/png": blob })]);
      }
      notify("已复制到剪贴板");
    } catch (error) {
      notify(errorMessage(error));
    }
  }, [exportImage, notify]);

  const handlePin = useCallback(async () => {
    if (!image) return;
    try {
      if (!isTauri()) throw new Error("窗口置顶需要在桌面应用中使用");
      const next = !isPinned;
      await setMainPinned(next);
      setIsPinned(next);
      notify(next ? "已置顶，可继续编辑" : "已取消置顶");
    } catch (error) {
      notify(errorMessage(error));
    }
  }, [image, isPinned, notify]);

  const handleSaveImage = useCallback(async () => {
    setPanel("none");
    try {
      const quality = isLossyFormat(exportFormat) ? exportQuality : 1;
      await saveImageAs(await exportImage(exportFormat, quality), title, exportFormat);
      notify(`${exportFormat.toUpperCase()} 已保存`);
    } catch (error) {
      notify(errorMessage(error));
    }
  }, [exportFormat, exportQuality, exportImage, notify, title]);

  const projectSnapshot = useCallback((): ScreenPinProject => {
    if (!image) throw new Error("还没有可保存的工程");
    return {
      format: "screenpin-project",
      version: 1,
      title,
      createdAt,
      updatedAt: new Date().toISOString(),
      image,
      annotations: history.annotations,
    };
  }, [createdAt, history.annotations, image, title]);

  const handleSaveProject = useCallback(async () => {
    setPanel("none");
    try {
      await saveProject(projectSnapshot());
      notify("可编辑工程已保存");
    } catch (error) {
      notify(errorMessage(error));
    }
  }, [notify, projectSnapshot]);

  const handleClose = useCallback(async () => {
    setPanel("none");
    if (!isTauri()) return;
    try {
      await hideWindow();
    } catch (error) {
      notify(errorMessage(error));
    }
  }, [notify]);
  const loadProject = useCallback(
    async (project: ScreenPinProject) => {
      if (!isScreenPinProject(project)) throw new Error("不是有效的 ScreenPin 工程");
      const fitted = zoomToFit(project.image, await refreshArea(), barHeightRef.current);
      setImage(project.image);
      setTitle(project.title);
      setCreatedAt(project.createdAt);
      history.reset(project.annotations);
      setSelectedId(null);
      setTool("select");
      setPanel("none");
      setZoom(fitted);
      recenter.current = true;
      setPixelatedImage(await pixelateImage(project.image.dataUrl, project.image.width, project.image.height));
    },
    [history.reset, refreshArea],
  );

  const handleOpenProject = useCallback(async () => {
    setPanel("none");
    if (!isTauri()) {
      projectInput.current?.click();
      return;
    }
    try {
      const project = await openProject();
      if (project) await loadProject(project);
    } catch (error) {
      notify(errorMessage(error));
    }
  }, [loadProject, notify]);

  const removeSelected = useCallback(() => {
    if (!selectedId) return;
    history.commit(history.annotations.filter((annotation) => annotation.id !== selectedId));
    setSelectedId(null);
  }, [history, selectedId]);

  const moveLayer = (direction: -1 | 1) => {
    if (!selectedId) return;
    const index = history.annotations.findIndex((annotation) => annotation.id === selectedId);
    const target = index + direction;
    if (index < 0 || target < 0 || target >= history.annotations.length) return;
    const next = [...history.annotations];
    [next[index], next[target]] = [next[target], next[index]];
    history.commit(next);
  };

  // The style panel edits the selection when there is one, otherwise the preset
  // used by the next annotation of the active tool.
  const styleTarget: Annotation["type"] | null = selected?.type ?? (tool === "select" ? null : tool);
  const activeStyle: AnnotationStyle | null = selected
    ? { color: selected.color, strokeWidth: selected.strokeWidth, opacity: selected.opacity, fontSize: selected.fontSize }
    : styleTarget
      ? styles[styleTarget]
      : null;

  const changeStyle = (change: Partial<AnnotationStyle>) => {
    if (!styleTarget) return;
    setStyles((current) => ({ ...current, [styleTarget]: { ...current[styleTarget], ...change } }));
    if (selectedId) {
      history.commit(
        history.annotations.map((annotation) => (annotation.id === selectedId ? { ...annotation, ...change } : annotation)),
      );
    }
  };

  const updateSelectedText = (text: string) => {
    if (!selectedId) return;
    history.commit(
      history.annotations.map((annotation) => (annotation.id === selectedId ? { ...annotation, text } : annotation)),
    );
  };
  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      const target = event.target as HTMLElement;
      if (["INPUT", "TEXTAREA"].includes(target.tagName)) return;
      const modifier = event.ctrlKey || event.metaKey;
      const key = event.key.toLowerCase();
      if (modifier && key === "z") {
        event.preventDefault();
        event.shiftKey ? history.redo() : history.undo();
        return;
      }
      if (modifier && key === "y") {
        event.preventDefault();
        history.redo();
        return;
      }
      if (modifier && key === "c" && image) {
        event.preventDefault();
        void handleCopy();
        return;
      }
      if (modifier && key === "s" && image) {
        event.preventDefault();
        void (event.shiftKey ? handleSaveProject() : handleSaveImage());
        return;
      }
      if (modifier && (key === "=" || key === "+")) {
        event.preventDefault();
        applyZoom(zoom + 0.1);
        return;
      }
      if (modifier && key === "-") {
        event.preventDefault();
        applyZoom(zoom - 0.1);
        return;
      }
      if (modifier && key === "0") {
        event.preventDefault();
        applyZoom(1);
        return;
      }
      if (modifier) return;
      if (event.key === "Delete" || event.key === "Backspace") removeSelected();
      if (event.key === "Escape") {
        if (panel !== "none") {
          setPanel("none");
          return;
        }
        setSelectedId(null);
        setTool("select");
      }
      if (key === "?") {
        setPanel((current) => (current === "shortcuts" ? "none" : "shortcuts"));
        return;
      }
      const item = TOOL_ITEMS.find((candidate) => candidate.key.toLowerCase() === key);
      if (item) setTool(item.id);
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [applyZoom, handleCopy, handleSaveImage, handleSaveProject, history.redo, history.undo, image, panel, removeSelected, zoom]);

  // Dragging the picture itself moves the window, the way a pinned note behaves.
  const dragOrigin = useRef<{ x: number; y: number } | null>(null);
  const startedDrag = useRef(false);

  const onStagePointerDown = (event: React.PointerEvent<HTMLDivElement>) => {
    const target = event.target as Element;
    if (event.button !== 0 || tool !== "select" || !target.classList?.contains("editor-canvas")) return;
    dragOrigin.current = { x: event.clientX, y: event.clientY };
    startedDrag.current = false;
  };

  const onStagePointerMove = (event: React.PointerEvent<HTMLDivElement>) => {
    const origin = dragOrigin.current;
    if (!origin || startedDrag.current) return;
    if (Math.abs(event.clientX - origin.x) < 4 && Math.abs(event.clientY - origin.y) < 4) return;
    startedDrag.current = true;
    dragOrigin.current = null;
    if (isTauri()) void startWindowDrag().catch(() => undefined);
  };

  const endStageDrag = () => {
    dragOrigin.current = null;
  };

  const onBarPointerDown = (event: React.PointerEvent<HTMLDivElement>) => {
    const target = event.target as HTMLElement;
    if (event.button !== 0 || target.closest("button, input, textarea")) return;
    if (isTauri()) void startWindowDrag().catch(() => undefined);
  };
  if (!image) {
    return (
      <div className="pin-window is-empty">
        <EmptyState
          busy={busy}
          onDrag={onBarPointerDown}
          onCapture={handleCapture}
          onOpen={() => imageInput.current?.click()}
          onOpenProject={handleOpenProject}
          onClose={handleClose}
        />
        <FilePickers
          imageInput={imageInput}
          projectInput={projectInput}
          onImage={loadCapture}
          onProject={loadProject}
          onError={notify}
        />
        {toast && <Toast message={toast} onDismiss={() => setToast(null)} />}
      </div>
    );
  }

  return (
    <div className="pin-window" style={{ "--bar-height": `${barHeight}px` } as React.CSSProperties}>
      <div
        ref={stageRef}
        className="pin-stage"
        style={{ width: stage!.width, height: stage!.height }}
        onPointerDown={onStagePointerDown}
        onPointerMove={onStagePointerMove}
        onPointerUp={endStageDrag}
        onPointerLeave={endStageDrag}
      >
        <EditorCanvas
          ref={canvasRef}
          image={image}
          pixelatedImage={pixelatedImage || image.dataUrl}
          annotations={history.annotations}
          selectedId={selectedId}
          tool={tool}
          style={styles[tool === "select" ? "rectangle" : tool]}
          onSelect={setSelectedId}
          onToolChange={setTool}
          onBegin={history.beginTransaction}
          onTransientChange={history.setTransient}
          onEnd={history.endTransaction}
        />
        {toast && <Toast message={toast} onDismiss={() => setToast(null)} />}
      </div>

      {panel === "style" && activeStyle && styleTarget && (
        <StylePanel
          style={activeStyle}
          target={styleTarget}
          hasSelection={Boolean(selected)}
          text={selected?.type === "text" ? selected.text ?? "" : null}
          onChange={changeStyle}
          onText={updateSelectedText}
          onMoveLayer={moveLayer}
          onDelete={removeSelected}
        />
      )}

      {panel === "menu" && (
        <MenuPanel
          title={title}
          image={image}
          zoom={zoom}
          busy={busy}
          onTitle={setTitle}
          onCapture={handleCapture}
          onPaste={handlePaste}
          onOpenImage={() => imageInput.current?.click()}
          onOpenProject={handleOpenProject}
          onSaveProject={handleSaveProject}
          onZoom={applyZoom}
          onZoomToScreen={zoomToScreen}
          exportFormat={exportFormat}
          exportQuality={exportQuality}
          onExportFormat={setExportFormat}
          onExportQuality={setExportQuality}
          onSaveImage={handleSaveImage}
        />
      )}

      {panel === "shortcuts" && <ShortcutsPanel />}
      <div ref={barRef} className="pin-bar" aria-label="标注工具" onPointerDown={onBarPointerDown}>
        {TOOL_ITEMS.map((item) => (
          <ToolButton
            key={item.id}
            item={item}
            active={tool === item.id}
            onClick={() => {
              setTool(item.id);
              if (item.id === "select") setPanel("none");
            }}
          />
        ))}
        <span className="bar-divider" />
        <button
          className={`icon-button swatch-button ${panel === "style" ? "is-active" : ""}`}
          title={activeStyle ? "样式" : "选中标注或先选择工具"}
          aria-label="样式"
          disabled={!activeStyle}
          onClick={() => setPanel((current) => (current === "style" ? "none" : "style"))}
        >
          <Palette size={17} />
          {activeStyle && <span className="swatch-dot" style={{ backgroundColor: activeStyle.color }} />}
        </button>
        <IconButton icon={Undo2} label="撤销" disabled={!history.canUndo} onClick={history.undo} />
        <IconButton icon={Redo2} label="重做" disabled={!history.canRedo} onClick={history.redo} />
        <span className="bar-divider" />
        <IconButton
          icon={HelpCircle}
          label="快捷键 (?)"
          active={panel === "shortcuts"}
          onClick={() => setPanel((current) => (current === "shortcuts" ? "none" : "shortcuts"))}
        />
        <IconButton icon={isPinned ? PinOff : Pin} label={isPinned ? "取消置顶" : "置顶"} active={isPinned} onClick={handlePin} />
        <IconButton icon={Download} label={`导出 ${exportFormat.toUpperCase()} (Ctrl+S)`} onClick={handleSaveImage} />
        <IconButton
          icon={MoreHorizontal}
          label="更多"
          active={panel === "menu"}
          onClick={() => setPanel((current) => (current === "menu" ? "none" : "menu"))}
        />
        <button className="primary-action" onClick={handleCopy}><Copy size={15} />复制</button>
        <IconButton icon={X} label="关闭到托盘" onClick={handleClose} />
      </div>

      <FilePickers
        imageInput={imageInput}
        projectInput={projectInput}
        onImage={loadCapture}
        onProject={loadProject}
        onError={notify}
      />
    </div>
  );
}

function IconButton({ icon: Icon, label, active, disabled, onClick }: { icon: LucideIcon; label: string; active?: boolean; disabled?: boolean; onClick: () => void }) {
  return <button className={`icon-button ${active ? "is-active" : ""}`} title={label} aria-label={label} aria-pressed={active} disabled={disabled} onClick={onClick}><Icon size={17} /></button>;
}

function ToolButton({ item, active, onClick }: { item: (typeof TOOL_ITEMS)[number]; active: boolean; onClick: () => void }) {
  const Icon = item.icon;
  return (
    <button className={`tool-button ${active ? "is-active" : ""}`} title={`${item.label} (${item.key})`} aria-label={item.label} aria-pressed={active} onClick={onClick}>
      <Icon size={17} />
    </button>
  );
}

function Toast({ message, onDismiss }: { message: string; onDismiss: () => void }) {
  return (
    <div className="toast">
      <Check size={14} />
      {message}
      <button aria-label="关闭提示" onClick={onDismiss}><X size={13} /></button>
    </div>
  );
}
function StylePanel({
  style,
  target,
  hasSelection,
  text,
  onChange,
  onText,
  onMoveLayer,
  onDelete,
}: {
  style: AnnotationStyle;
  target: Annotation["type"];
  hasSelection: boolean;
  text: string | null;
  onChange: (change: Partial<AnnotationStyle>) => void;
  onText: (text: string) => void;
  onMoveLayer: (direction: -1 | 1) => void;
  onDelete: () => void;
}) {
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
          onChange={(strokeWidth) => onChange({ strokeWidth })}
        />
      )}
      {target === "text" && (
        <Slider label="字号" value={style.fontSize} min={12} max={96} suffix="px" onChange={(fontSize) => onChange({ fontSize })} />
      )}
      <Slider
        label="不透明度"
        value={Math.round(style.opacity * 100)}
        min={10}
        max={100}
        suffix="%"
        onChange={(value) => onChange({ opacity: value / 100 })}
      />
      {text !== null && (
        <label className="panel-field">
          <span>文字</span>
          <textarea value={text} rows={2} onChange={(event) => onText(event.target.value)} />
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

function Slider({ label, value, min, max, suffix, onChange }: { label: string; value: number; min: number; max: number; suffix: string; onChange: (value: number) => void }) {
  return (
    <label className="panel-slider">
      <span>{label}</span>
      <output>{value}{suffix}</output>
      <input type="range" min={min} max={max} value={value} onChange={(event) => onChange(Number(event.target.value))} />
    </label>
  );
}
function MenuPanel({
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
}: {
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
}) {
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

function EmptyState({
  busy,
  onDrag,
  onCapture,
  onOpen,
  onOpenProject,
  onClose,
}: {
  busy: boolean;
  onDrag: (event: React.PointerEvent<HTMLDivElement>) => void;
  onCapture: () => void;
  onOpen: () => void;
  onOpenProject: () => void;
  onClose: () => void;
}) {
  return (
    <div className="empty-state" onPointerDown={onDrag}>
      <button className="empty-close" title="关闭到托盘" aria-label="关闭到托盘" onClick={onClose}><X size={15} /></button>
      <div className="empty-symbol"><Aperture size={26} /></div>
      <div className="empty-actions">
        <button className="primary-action large" disabled={busy} onClick={onCapture}><ScanLine size={16} />截取屏幕</button>
        <button className="secondary-action" onClick={onOpen}><ImagePlus size={16} />打开图片</button>
        <button className="secondary-action" onClick={onOpenProject}><FolderOpen size={16} />工程</button>
      </div>
      <div className="shortcut-row"><kbd>Ctrl</kbd><span>+</span><kbd>Shift</kbd><span>+</span><kbd>A</kbd></div>
    </div>
  );
}
function FilePickers({
  imageInput,
  projectInput,
  onImage,
  onProject,
  onError,
}: {
  imageInput: React.RefObject<HTMLInputElement | null>;
  projectInput: React.RefObject<HTMLInputElement | null>;
  onImage: (capture: CaptureResult, name?: string) => Promise<void>;
  onProject: (project: ScreenPinProject) => Promise<void>;
  onError: (message: string) => void;
}) {
  return (
    <>
      <input
        ref={imageInput}
        hidden
        type="file"
        accept="image/png,image/jpeg,image/webp"
        onChange={async (event) => {
          const file = event.target.files?.[0];
          event.target.value = "";
          if (!file) return;
          try {
            await onImage(await imageFromFile(file), file.name);
          } catch (error) {
            onError(errorMessage(error));
          }
        }}
      />
      <input
        ref={projectInput}
        hidden
        type="file"
        accept=".screenpin,application/json"
        onChange={async (event) => {
          const file = event.target.files?.[0];
          event.target.value = "";
          if (!file) return;
          try {
            await onProject(JSON.parse(await file.text()));
          } catch (error) {
            onError(errorMessage(error));
          }
        }}
      />
    </>
  );
}

function toolName(tool: Annotation["type"]) {
  return TOOL_ITEMS.find((item) => item.id === tool)?.label ?? tool;
}

function errorMessage(error: unknown) {
  return error instanceof Error ? error.message : String(error);
}

function ShortcutsPanel() {
  const shortcuts = [
    { keys: ["V"], action: "选择工具" },
    { keys: ["R"], action: "矩形" },
    { keys: ["O"], action: "椭圆" },
    { keys: ["A"], action: "箭头" },
    { keys: ["P"], action: "画笔" },
    { keys: ["H"], action: "荧光笔" },
    { keys: ["T"], action: "文字" },
    { keys: ["M"], action: "马赛克" },
    { keys: ["Ctrl", "Z"], action: "撤销" },
    { keys: ["Ctrl", "Shift", "Z"], action: "重做" },
    { keys: ["Ctrl", "Y"], action: "重做" },
    { keys: ["Ctrl", "C"], action: "复制到剪贴板" },
    { keys: ["Ctrl", "S"], action: "导出 PNG" },
    { keys: ["Ctrl", "Shift", "S"], action: "保存工程" },
    { keys: ["Ctrl", "+"], action: "放大" },
    { keys: ["Ctrl", "-"], action: "缩小" },
    { keys: ["Ctrl", "0"], action: "重置缩放" },
    { keys: ["Delete"], action: "删除所选" },
    { keys: ["Backspace"], action: "删除所选" },
    { keys: ["Esc"], action: "取消选择/关闭面板" },
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
              {shortcut.keys.map((key, i) => (
                <span key={i}>
                  <kbd>{key}</kbd>
                  {i < shortcut.keys.length - 1 && <span className="shortcut-plus">+</span>}
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
