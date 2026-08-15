import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
  type PointerEvent,
} from "react";
import { listen } from "@tauri-apps/api/event";
import { EmptyState } from "./components/EmptyState";
import { EditorToolbar } from "./components/EditorToolbar";
import { Toast } from "./components/EditorControls";
import { FilePickers } from "./components/FilePickers";
import {
  MenuPanel,
  SettingsPanel,
  ShortcutsPanel,
  StylePanel,
} from "./components/EditorPanels";
import { EditorCanvas, type EditorCanvasHandle } from "./EditorCanvas";
import {
  CAPTURE_SETTLE_DELAY,
  DRAW_TOOLS,
  errorMessage,
  type EditorPanel,
  zoomToFit,
} from "./editor/constants";
import { useEditorShortcuts } from "./hooks/useEditorShortcuts";
import { useEditorWindowLayout } from "./hooks/useEditorWindowLayout";
import {
  captureScreen,
  confirmDiscardChanges,
  copyImage,
  createDemoImage,
  getConfig,
  hideWindow,
  imageFromFile,
  isTauri,
  openProject,
  pasteImage,
  pixelateImage,
  quitApp,
  saveConfig,
  saveImageAs,
  saveProject,
  setMainPinned,
  showMainWindow,
  startWindowDrag,
  validateProjectImage,
  type AppConfig,
} from "./platform";
import type {
  Annotation,
  AnnotationStyle,
  CaptureResult,
  ExportFormat,
  ImageSource,
  SavedDocument,
  ScreenPinProject,
  Tool,
} from "./types";
import { defaultStyle, isDocumentDirty, isLossyFormat, isScreenPinProject } from "./types";
import { useEditorHistory } from "./useEditorHistory";
export default function App() {
  const canvasRef = useRef<EditorCanvasHandle>(null);
  const imageInput = useRef<HTMLInputElement>(null);
  const projectInput = useRef<HTMLInputElement>(null);
  const demoCapture = useMemo(() => (new URLSearchParams(window.location.search).has("demo") ? createDemoImage() : null), []);
  const [image, setImage] = useState<ImageSource | null>(() => demoCapture ? { ...demoCapture, name: "交付概览" } : null);
  const [pixelatedImage, setPixelatedImage] = useState<string | null>(null);
  const [mosaicStatus, setMosaicStatus] = useState<"loading" | "ready" | "error">("loading");
  const [title, setTitle] = useState(demoCapture ? "交付概览" : "未命名截图");
  const [createdAt, setCreatedAt] = useState(() => new Date().toISOString());
  const [tool, setTool] = useState<Tool>("select");
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [zoom, setZoom] = useState(1);
  const [isPinned, setIsPinned] = useState(false);
  const [busy, setBusy] = useState(false);
  const [toast, setToast] = useState<string | null>(null);
  const [panel, setPanel] = useState<EditorPanel>("none");
  const [styles, setStyles] = useState<Record<Annotation["type"], AnnotationStyle>>(() =>
    DRAW_TOOLS.reduce(
      (all, id) => ({ ...all, [id]: defaultStyle(id) }),
      {} as Record<Annotation["type"], AnnotationStyle>,
    ),
  );
  const [exportFormat, setExportFormat] = useState<ExportFormat>("png");
  const [exportQuality, setExportQuality] = useState(0.92);
  const [appConfig, setAppConfig] = useState<AppConfig | null>(null);
  const pixelationRequest = useRef(0);
  const captureInProgress = useRef(false);
  const history = useEditorHistory();
  const [savedDocument, setSavedDocument] = useState<SavedDocument | null>(null);
  const {
    applyZoom,
    barHeight,
    barHeightRef,
    barRef,
    refreshArea,
    requestRecenter,
    stage,
    stageRef,
    waitForWindowFit,
    zoomToScreen,
  } = useEditorWindowLayout({ image, zoom, setZoom });

  const selected = useMemo(
    () => history.annotations.find((annotation) => annotation.id === selectedId) ?? null,
    [history.annotations, selectedId],
  );
  const hasUnsavedChanges = isDocumentDirty(image, title, createdAt, history.annotations, savedDocument);

  const confirmReplace = useCallback(async () => {
    if (!hasUnsavedChanges) return true;
    return confirmDiscardChanges();
  }, [hasUnsavedChanges]);

  const notify = useCallback((message: string) => {
    setToast(message);
    window.setTimeout(() => setToast((current) => (current === message ? null : current)), 2400);
  }, []);

  useEffect(() => {
    void refreshArea();
    if (isTauri()) {
      void getConfig().then(setAppConfig).catch(() => setAppConfig({ captureShortcut: "Super+Shift+S" }));
    }
  }, [refreshArea]);
  const loadCapture = useCallback(
    async (capture: CaptureResult, name = `截图 ${new Date().toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit" })}`) => {
      const request = ++pixelationRequest.current;
      setPixelatedImage(null);
      setMosaicStatus("loading");
      const source = { ...capture, name };
      const fitted = zoomToFit(source, await refreshArea(), barHeightRef.current);
      if (request !== pixelationRequest.current) return;
      setImage(source);
      setTitle(name.replace(/\.[^.]+$/, ""));
      setCreatedAt(new Date().toISOString());
      setSelectedId(null);
      setTool("select");
      setPanel("none");
      history.reset();
      setSavedDocument(null);
      setZoom(fitted);
      requestRecenter();
      try {
        const result = await pixelateImage(capture.dataUrl, capture.width, capture.height);
        if (request !== pixelationRequest.current) return;
        setPixelatedImage(result);
        setMosaicStatus("ready");
      } catch {
        if (request !== pixelationRequest.current) return;
        setPixelatedImage(null);
        setMosaicStatus("error");
        notify("马赛克处理失败，已禁用该工具");
      }
    },
    [history.reset, notify, refreshArea, requestRecenter],
  );

  useEffect(() => {
    if (demoCapture) loadCapture(demoCapture, "交付概览");
  }, [demoCapture, loadCapture]);

  const handleCapture = useCallback(async () => {
    setPanel("none");
    if (!isTauri()) {
      imageInput.current?.click();
      return;
    }
    if (captureInProgress.current) return;
    captureInProgress.current = true;
    try {
      if (!(await confirmReplace())) return;
      setBusy(true);
      const capture = await captureScreen();
      // The compositor may still be tearing down the system screenshot UI.
      // Let that transition settle before the hidden editor is re-fitted.
      await new Promise<void>((resolve) => window.setTimeout(resolve, CAPTURE_SETTLE_DELAY));
      await loadCapture(capture);
      // `capture_screen` keeps the window hidden until the new geometry has
      // been applied, so revealing it here cannot restore the old position.
      // Yield once so React has committed the image and run the layout effect,
      // then wait for that native geometry request before showing the window.
      await new Promise<void>((resolve) => window.setTimeout(resolve, 0));
      await waitForWindowFit();
      await showMainWindow();
    } catch (error) {
      // Capture failures can happen after the native window was hidden. Make
      // the editor recoverable even when the command failed mid-flight.
      await showMainWindow().catch(() => undefined);
      notify(errorMessage(error));
    } finally {
      captureInProgress.current = false;
      setBusy(false);
    }
  }, [confirmReplace, loadCapture, notify, waitForWindowFit]);

  useEffect(() => {
    if (!isTauri()) return;
    const unlisten = listen("capture-requested", handleCapture);
    return () => {
      void unlisten.then((dispose) => dispose());
    };
  }, [handleCapture]);

  const handlePaste = useCallback(async () => {
    setPanel("none");
    if (!(await confirmReplace())) return;
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
  }, [confirmReplace, loadCapture, notify]);
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
      const saved = await saveImageAs(await exportImage(exportFormat, quality), title, exportFormat);
      if (saved) notify(`${exportFormat.toUpperCase()} 已保存`);
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
      const project = projectSnapshot();
      if (await saveProject(project)) {
        setSavedDocument({
          imageDataUrl: project.image.dataUrl,
          title: project.title,
          createdAt: project.createdAt,
          annotations: JSON.stringify(project.annotations),
        });
        notify("可编辑工程已保存");
      }
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
      await validateProjectImage(project.image);
      const request = ++pixelationRequest.current;
      setPixelatedImage(null);
      setMosaicStatus("loading");
      const fitted = zoomToFit(project.image, await refreshArea(), barHeightRef.current);
      if (request !== pixelationRequest.current) return;
      setImage(project.image);
      setTitle(project.title);
      setCreatedAt(project.createdAt);
      history.reset(project.annotations);
      setSavedDocument({
        imageDataUrl: project.image.dataUrl,
        title: project.title,
        createdAt: project.createdAt,
        annotations: JSON.stringify(project.annotations),
      });
      setSelectedId(null);
      setTool("select");
      setPanel("none");
      setZoom(fitted);
      requestRecenter();
      try {
        const result = await pixelateImage(project.image.dataUrl, project.image.width, project.image.height);
        if (request !== pixelationRequest.current) return;
        setPixelatedImage(result);
        setMosaicStatus("ready");
      } catch {
        if (request !== pixelationRequest.current) return;
        setPixelatedImage(null);
        setMosaicStatus("error");
        notify("马赛克处理失败，已阻止含马赛克的导出");
      }
    },
    [history.reset, notify, refreshArea, requestRecenter],
  );

  const handleOpenProject = useCallback(async () => {
    setPanel("none");
    if (!isTauri()) {
      projectInput.current?.click();
      return;
    }
    try {
      const project = await openProject();
      if (project && (await confirmReplace())) await loadProject(project);
    } catch (error) {
      notify(errorMessage(error));
    }
  }, [confirmReplace, loadProject, notify]);

  useEffect(() => {
    if (!isTauri()) return;
    const unlisten = listen("quit-requested", async () => {
      if (await confirmReplace()) await quitApp();
    });
    return () => {
      void unlisten.then((dispose) => dispose());
    };
  }, [confirmReplace]);

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

  const changeStyle = (change: Partial<AnnotationStyle>, transient = false) => {
    if (!styleTarget) return;
    setStyles((current) => ({ ...current, [styleTarget]: { ...current[styleTarget], ...change } }));
    if (selectedId) {
      const next = history.annotations.map((annotation) =>
        annotation.id === selectedId ? { ...annotation, ...change } : annotation,
      );
      transient ? history.setTransient(next) : history.commit(next);
    }
  };

  const updateSelectedText = (text: string) => {
    if (!selectedId) return;
    history.setTransient(
      history.annotations.map((annotation) => (annotation.id === selectedId ? { ...annotation, text } : annotation)),
    );
  };

  useEditorShortcuts({
    applyZoom,
    image,
    mosaicStatus,
    notify,
    onCopy: handleCopy,
    onDeleteSelected: removeSelected,
    onSaveImage: handleSaveImage,
    onSaveProject: handleSaveProject,
    panel,
    redo: history.redo,
    setPanel,
    setSelectedId,
    setTool,
    undo: history.undo,
    zoom,
  });

  // Dragging the picture itself moves the window, the way a pinned note behaves.
  const dragOrigin = useRef<{ x: number; y: number } | null>(null);
  const startedDrag = useRef(false);

  const onStagePointerDown = (event: PointerEvent<HTMLDivElement>) => {
    const target = event.target as Element;
    if (event.button !== 0 || tool !== "select" || !target.classList?.contains("editor-canvas")) return;
    dragOrigin.current = { x: event.clientX, y: event.clientY };
    startedDrag.current = false;
  };

  const onStagePointerMove = (event: PointerEvent<HTMLDivElement>) => {
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

  const onBarPointerDown = (event: PointerEvent<HTMLDivElement>) => {
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
          onBeforeReplace={confirmReplace}
          onError={notify}
        />
        {toast && <Toast message={toast} onDismiss={() => setToast(null)} />}
      </div>
    );
  }

  return (
    <div className="pin-window" style={{ "--bar-height": `${barHeight}px` } as CSSProperties}>
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
          pixelatedImage={pixelatedImage}
          annotations={history.annotations}
          selectedId={selectedId}
          tool={tool}
          zoom={zoom}
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
          onBeginChange={history.beginTransaction}
          onEndChange={history.endTransaction}
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

      {panel === "shortcuts" && <ShortcutsPanel exportFormat={exportFormat} captureShortcut={appConfig?.captureShortcut} />}

      {panel === "settings" && appConfig && (
        <SettingsPanel
          config={appConfig}
          onSave={async (newConfig) => {
            try {
              await saveConfig(newConfig);
              setAppConfig(newConfig);
              setPanel("none");
              notify("设置已保存");
            } catch (error) {
              notify(errorMessage(error));
            }
          }}
          onClose={() => setPanel("none")}
        />
      )}

      <EditorToolbar
        activeStyle={activeStyle}
        barRef={barRef}
        canRedo={history.canRedo}
        canUndo={history.canUndo}
        exportFormat={exportFormat}
        isPinned={isPinned}
        mosaicStatus={mosaicStatus}
        onClose={handleClose}
        onCopy={handleCopy}
        onPanelChange={setPanel}
        onPin={handlePin}
        onRedo={history.redo}
        onSaveImage={handleSaveImage}
        onToolChange={setTool}
        onUndo={history.undo}
        onPointerDown={onBarPointerDown}
        panel={panel}
        tool={tool}
      />

      <FilePickers
        imageInput={imageInput}
        projectInput={projectInput}
        onImage={loadCapture}
        onProject={loadProject}
        onBeforeReplace={confirmReplace}
        onError={notify}
      />
    </div>
  );
}
