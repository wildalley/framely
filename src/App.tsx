import { Fragment, useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
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
  ScanText,
  Settings,
  Square,
  SquareDashed,
  Trash2,
  Type,
  Undo2,
  X,
  ZoomIn,
  ZoomOut,
} from "lucide-react";
import type { LucideIcon } from "lucide-react";
import { listen } from "@tauri-apps/api/event";
import { CaptureOverlay } from "./CaptureOverlay";
import { ScrollCaptureOverlay } from "./ScrollCapture";
import { EditorCanvas, type EditorCanvasHandle } from "./EditorCanvas";
import { formatShortcut, MODIFIER_ORDER, mainShortcutKey, modifierFromEvent, shortcutParts } from "./shortcut";
import {
  captureScreen,
  captureWithSystem,
  confirmDiscardChanges,
  copyImage,
  copyText,
  createDemoImage,
  enterCaptureMode,
  exitCaptureMode,
  fitWindow,
  getConfig,
  hideWindow,
  imageFromFile,
  isTauri,
  openProject,
  pasteImage,
  pixelateImage,
  ocrImage,
  quitApp,
  saveConfig,
  saveImageAs,
  saveProject,
  setMainPinned,
  showMainWindow,
  startWindowDrag,
  stitchImages,
  validateProjectImage,
  workArea,
  type AppConfig,
  type StitchDirection,
} from "./platform";
import type {
  Annotation,
  AnnotationStyle,
  CaptureResponse,
  CaptureResult,
  ExportFormat,
  ImageSource,
  SavedDocument,
  ScreenPinProject,
  Tool,
} from "./types";
import { COLORS, EXPORT_FORMATS, defaultStyle, isDocumentDirty, isLossyFormat, isScreenPinProject } from "./types";
import { useEditorHistory } from "./useEditorHistory";

const TOOL_ITEMS: { id: Tool; label: string; icon: LucideIcon; key: string }[] = [
  { id: "select", label: "选择", icon: MousePointer2, key: "V" },
  { id: "rectangle", label: "矩形", icon: Square, key: "R" },
  { id: "ellipse", label: "椭圆", icon: Circle, key: "O" },
  { id: "arrow", label: "箭头", icon: MoveUpRight, key: "A" },
  { id: "pen", label: "画笔", icon: Pencil, key: "P" },
  { id: "highlighter", label: "荧光笔", icon: Highlighter, key: "H" },
  { id: "area-highlight", label: "区域高亮", icon: SquareDashed, key: "G" },
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
  const stitchFileInput = useRef<HTMLInputElement>(null);
  const demoCapture = useMemo(() => (new URLSearchParams(window.location.search).has("demo") ? createDemoImage() : null), []);
  const [image, setImage] = useState<ImageSource | null>(() => demoCapture ? { ...demoCapture, name: "交付概览" } : null);
  const [captureDraft, setCaptureDraft] = useState<CaptureResponse | null>(null);
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
  const [panel, setPanel] = useState<"none" | "style" | "menu" | "shortcuts" | "settings" | "ocr" | "stitch">("none");
  const [ocrText, setOcrText] = useState<string | null>(null);
  const [ocrSetupError, setOcrSetupError] = useState<string | null>(null);
  const [ocrBusy, setOcrBusy] = useState(false);
  const [stitchSources, setStitchSources] = useState<CaptureResult[]>([]);
  const [stitchDirection, setStitchDirection] = useState<StitchDirection>("vertical");
  const [scrollCaptureActive, setScrollCaptureActive] = useState(false);
  const [styles, setStyles] = useState<Record<Annotation["type"], AnnotationStyle>>(() =>
    DRAW_TOOLS.reduce(
      (all, id) => ({ ...all, [id]: defaultStyle(id) }),
      {} as Record<Annotation["type"], AnnotationStyle>,
    ),
  );
  const [exportFormat, setExportFormat] = useState<ExportFormat>("png");
  const [exportQuality, setExportQuality] = useState(0.92);
  const [appConfig, setAppConfig] = useState<AppConfig | null>(null);
  const [barHeight, setBarHeight] = useState(ASSUMED_BAR_HEIGHT);
  const area = useRef<{ width: number; height: number } | null>(null);
  const barHeightRef = useRef(barHeight);
  const recenter = useRef(true);
  const pixelationRequest = useRef(0);
  const captureInProgress = useRef(false);
  const history = useEditorHistory();
  const [savedDocument, setSavedDocument] = useState<SavedDocument | null>(null);

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
    if (isTauri()) {
      void getConfig().then(setAppConfig).catch(() => setAppConfig({ captureShortcut: "CommandOrControl+Shift+A" }));
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
      recenter.current = true;
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
    [history.reset, notify, refreshArea],
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
    // `image` is in the deps so loading a same-size image still re-fits the
    // window — needed after a capture confirm, where the geometry must be
    // re-applied even when the new screenshot happens to match the old size.
  }, [stage?.width, stage?.height, barHeight, image]);

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
    if (captureDraft || captureInProgress.current) return;
    setPanel("none");
    if (!isTauri()) {
      imageInput.current?.click();
      return;
    }
    captureInProgress.current = true;
    setBusy(true);
    try {
      const response = await captureScreen();
      if (!response.selectionRequired) {
        await loadCapture({ dataUrl: response.dataUrl, width: response.width, height: response.height });
        if (response.fallbackUsed) notify("应用内采集不可用，已使用系统截图");
        return;
      }

      setCaptureDraft(response);
      // Let React replace the editor with the selector before the hidden native
      // window is made fullscreen, avoiding a flash of the previous document.
      await new Promise<void>((resolve) => window.setTimeout(resolve, 0));
      try {
        await enterCaptureMode();
      } catch {
        setCaptureDraft(null);
        await exitCaptureMode(isPinned).catch(() => undefined);
        await new Promise<void>((resolve) => window.setTimeout(resolve, 150));
        await loadCapture(await captureWithSystem());
        if (isTauri()) await showMainWindow().catch(() => undefined);
        notify("应用内选区不可用，已切换系统截图");
      }
    } catch (error) {
      await exitCaptureMode(isPinned).catch(() => undefined);
      if (isTauri()) await showMainWindow().catch(() => undefined);
      notify(errorMessage(error));
    } finally {
      captureInProgress.current = false;
      setBusy(false);
    }
  }, [captureDraft, confirmReplace, isPinned, loadCapture, notify]);

  const cancelCaptureSelection = useCallback(async () => {
    setCaptureDraft(null);
    try {
      await exitCaptureMode(isPinned);
      // The compositor restores the window from fullscreen asynchronously; let
      // that settle before re-fitting geometry, or the restore can land after
      // fitWindow and leave the window at a stale position.
      await new Promise<void>((resolve) => window.setTimeout(resolve, 150));
      const size = stage
        ? { width: Math.max(MIN_WINDOW_WIDTH, stage.width), height: stage.height + barHeightRef.current }
        : EMPTY_WINDOW;
      await fitWindow(size.width, size.height, false);
      // exit_capture_mode leaves the window hidden so the fit above applies to
      // a settled window; show it now that its geometry is correct.
      if (isTauri()) await showMainWindow();
    } catch (error) {
      // Make sure the window is visible again even if the exit/fit failed.
      if (isTauri()) await showMainWindow().catch(() => undefined);
      notify(errorMessage(error));
    }
  }, [isPinned, notify, stage]);

  const confirmCaptureSelection = useCallback(async (cropped: CaptureResult) => {
    if (!captureDraft) return;
    setBusy(true);
    try {
      setCaptureDraft(null);
      await exitCaptureMode(isPinned);
      // Wait for the fullscreen-exit restore to finish so the editor window
      // opens centred on the picked screen instead of where the selector left
      // the window (see exit_capture_mode / the restore race).
      await new Promise<void>((resolve) => window.setTimeout(resolve, 150));
      await loadCapture(cropped);
      // The window is still hidden: the load above fitted its geometry, so
      // showing it now reveals the editor already centred on the picked screen.
      if (isTauri()) await showMainWindow();
    } catch (error) {
      setCaptureDraft(null);
      await exitCaptureMode(isPinned).catch(() => undefined);
      // Keep the window visible after a failed confirm.
      if (isTauri()) await showMainWindow().catch(() => undefined);
      notify(errorMessage(error));
    } finally {
      setBusy(false);
    }
  }, [captureDraft, isPinned, loadCapture, notify]);

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

  const handleOcr = useCallback(async () => {
    if (!image) return;
    setOcrBusy(true);
    setOcrText(null);
    setOcrSetupError(null);
    try {
      const result = await ocrImage(image.dataUrl);
      if (result.setupError) {
        setOcrSetupError(result.setupError);
        setOcrText("");
      } else {
        setOcrText(result.text);
      }
      setPanel("ocr");
    } catch (error) {
      setOcrText("");
      setOcrSetupError(errorMessage(error));
      setPanel("ocr");
    } finally {
      setOcrBusy(false);
    }
  }, [image]);

  const handleCopyOcr = useCallback(async () => {
    if (!ocrText) return;
    try {
      if (isTauri()) {
        await copyText(ocrText);
      } else {
        await navigator.clipboard.writeText(ocrText);
      }
      notify("文字已复制到剪贴板");
    } catch (error) {
      notify(errorMessage(error));
    }
  }, [notify, ocrText]);

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

  // Closing to the tray ends the editing session: drop the current document so
  // the next "显示窗口" opens a fresh welcome page instead of the previous
  // screenshot. The window is hidden at this point, so the fit that follows
  // centres the empty window while hidden and the tray-show reveals it there.
  const resetEditor = useCallback(() => {
    ++pixelationRequest.current;
    setPixelatedImage(null);
    setMosaicStatus("loading");
    setImage(null);
    setTitle("未命名截图");
    setCreatedAt(new Date().toISOString());
    setSelectedId(null);
    setTool("select");
    setPanel("none");
    setOcrText(null);
    setStitchSources([]);
    history.reset();
    setSavedDocument(null);
    setZoom(1);
    recenter.current = true;
  }, [history.reset]);

  useEffect(() => {
    if (!isTauri()) return;
    const unlisten = listen("closed-to-tray", resetEditor);
    return () => {
      void unlisten.then((dispose) => dispose());
    };
  }, [resetEditor]);
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
      recenter.current = true;
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
    [history.reset, notify, refreshArea],
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

  // Long screenshot: pick several images in order and join them into one image.
  // Long screenshot: gather images in the stitch panel, then join them.
  const addStitchFiles = useCallback(
    async (files: FileList | null) => {
      if (!files || files.length === 0) return;
      try {
        const sources = await Promise.all(Array.from(files).map(imageFromFile));
        setStitchSources((current) => [...current, ...sources]);
      } catch (error) {
        notify(errorMessage(error));
      }
    },
    [notify],
  );

  const confirmStitch = useCallback(async () => {
    if (stitchSources.length < 2) return;
    if (!(await confirmReplace())) return;
    setBusy(true);
    setPanel("none");
    try {
      const stitched = await stitchImages(stitchSources, stitchDirection);
      await loadCapture(stitched, `长截图 ${new Date().toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit" })}`);
      setStitchSources([]);
    } catch (error) {
      setPanel("stitch");
      notify(errorMessage(error));
    } finally {
      setBusy(false);
    }
  }, [confirmReplace, loadCapture, notify, stitchDirection, stitchSources]);

  const openScrollCapture = useCallback(async () => {
    setPanel("none");
    try {
      await enterCaptureMode();
      setScrollCaptureActive(true);
    } catch (error) {
      notify(errorMessage(error));
    }
  }, [notify]);

  const closeScrollCapture = useCallback(async () => {
    setScrollCaptureActive(false);
    try {
      await exitCaptureMode(isPinned);
      // Let the fullscreen-exit restore settle before the following
      // loadCapture re-fits the window geometry (same race as capture confirm).
      await new Promise<void>((resolve) => window.setTimeout(resolve, 150));
    } catch {
      // Non-fatal: the window may already be in its normal state.
    }
  }, [isPinned]);

  const finishScrollCapture = useCallback(
    async (result: CaptureResult) => {
      await closeScrollCapture();
      await loadCapture(result, `滚动截图 ${new Date().toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit" })}`);
      // The window is hidden at this point; reveal it after the fit above.
      if (isTauri()) await showMainWindow();
    },
    [closeScrollCapture, isTauri, loadCapture, showMainWindow],
  );

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
  useEffect(() => {
    if (captureDraft) return;
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
      if (item?.id === "mosaic" && mosaicStatus !== "ready") {
        notify(mosaicStatus === "loading" ? "马赛克正在准备" : "马赛克当前不可用");
        return;
      }
      if (item) setTool(item.id);
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [applyZoom, captureDraft, handleCopy, handleSaveImage, handleSaveProject, history.redo, history.undo, image, mosaicStatus, notify, panel, removeSelected, zoom]);

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
  if (scrollCaptureActive) {
    return (
      <ScrollCaptureOverlay
        onFinish={(result) => void finishScrollCapture(result)}
        onCancel={() => {
          void closeScrollCapture().then(() => {
            if (isTauri()) return showMainWindow();
          });
        }}
      />
    );
  }
  if (captureDraft) {
    return (
      <CaptureOverlay
        capture={captureDraft}
        screens={captureDraft.screens}
        onConfirm={confirmCaptureSelection}
        onCancel={cancelCaptureSelection}
      />
    );
  }
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
          captureShortcut={appConfig?.captureShortcut}
          onSettings={isTauri() ? () => setPanel((current) => (current === "settings" ? "none" : "settings")) : undefined}
        />
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
                throw error;
              }
            }}
            onClose={() => setPanel("none")}
          />
        )}
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
          onStitch={() => setPanel("stitch")}
          onScrollCapture={() => void openScrollCapture()}
        />
      )}

      {panel === "stitch" && (
        <>
          <input
            ref={stitchFileInput}
            hidden
            type="file"
            multiple
            accept="image/png,image/jpeg,image/webp"
            onChange={async (event) => {
              const files = event.target.files;
              event.target.value = "";
              if (!files || files.length === 0) return;
              await addStitchFiles(files);
            }}
          />
          <StitchPanel
            sources={stitchSources}
            direction={stitchDirection}
            busy={busy}
            onAddFiles={() => stitchFileInput.current?.click()}
            onDirection={setStitchDirection}
            onReorder={(from, to) =>
              setStitchSources((current) => {
                const next = [...current];
                const [moved] = next.splice(from, 1);
                next.splice(to, 0, moved);
                return next;
              })
            }
            onRemove={(index) => setStitchSources((current) => current.filter((_, i) => i !== index))}
            onStitch={() => void confirmStitch()}
            onClose={() => setPanel("none")}
          />
        </>
      )}

      {panel === "shortcuts" && <ShortcutsPanel exportFormat={exportFormat} captureShortcut={appConfig?.captureShortcut} />}

      {panel === "ocr" && (
        <OcrPanel
          busy={ocrBusy}
          text={ocrText}
          setupError={ocrSetupError}
          onRetry={() => void handleOcr()}
          onCopy={handleCopyOcr}
          onClose={() => setPanel("none")}
        />
      )}

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
              throw error;
            }
          }}
          onClose={() => setPanel("none")}
        />
      )}

      <div ref={barRef} className="pin-bar" aria-label="标注工具" onPointerDown={onBarPointerDown}>
        {TOOL_ITEMS.map((item) => (
          <ToolButton
            key={item.id}
            item={item}
            active={tool === item.id}
            disabled={item.id === "mosaic" && mosaicStatus !== "ready"}
            disabledReason={mosaicStatus === "loading" ? "马赛克正在准备" : "马赛克处理失败"}
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
        <IconButton
          icon={ScanText}
          label="识别文字 (OCR)"
          active={panel === "ocr"}
          disabled={!image || ocrBusy}
          onClick={() => void handleOcr()}
        />
        {isTauri() && (
          <IconButton
            icon={Settings}
            label="设置"
            active={panel === "settings"}
            onClick={() => setPanel((current) => (current === "settings" ? "none" : "settings"))}
          />
        )}
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
        onBeforeReplace={confirmReplace}
        onError={notify}
      />
    </div>
  );
}

function IconButton({ icon: Icon, label, active, disabled, onClick }: { icon: LucideIcon; label: string; active?: boolean; disabled?: boolean; onClick: () => void }) {
  return <button className={`icon-button ${active ? "is-active" : ""}`} title={label} aria-label={label} aria-pressed={active} disabled={disabled} onClick={onClick}><Icon size={17} /></button>;
}

function ToolButton({ item, active, disabled, disabledReason, onClick }: { item: (typeof TOOL_ITEMS)[number]; active: boolean; disabled?: boolean; disabledReason?: string; onClick: () => void }) {
  const Icon = item.icon;
  return (
    <button className={`tool-button ${active ? "is-active" : ""}`} title={disabled ? disabledReason : `${item.label} (${item.key})`} aria-label={item.label} aria-pressed={active} disabled={disabled} onClick={onClick}>
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
  onBeginChange,
  onEndChange,
  onMoveLayer,
  onDelete,
}: {
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
          onChange={(strokeWidth) => onChange({ strokeWidth }, true)}
          onBegin={onBeginChange}
          onEnd={onEndChange}
        />
      )}
      {target === "text" && (
        <Slider label="字号" value={style.fontSize} min={12} max={96} suffix="px" onChange={(fontSize) => onChange({ fontSize }, true)} onBegin={onBeginChange} onEnd={onEndChange} />
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
          <textarea value={text} rows={2} onFocus={onBeginChange} onChange={(event) => onText(event.target.value)} onBlur={() => onEndChange()} />
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

function Slider({ label, value, min, max, suffix, onChange, onBegin, onEnd }: { label: string; value: number; min: number; max: number; suffix: string; onChange: (value: number) => void; onBegin?: () => void; onEnd?: () => void }) {
  return (
    <label className="panel-slider">
      <span>{label}</span>
      <output>{value}{suffix}</output>
      <input
        type="range"
        min={min}
        max={max}
        value={value}
        onFocus={() => onBegin?.()}
        onPointerDown={() => onBegin?.()}
        onChange={(event) => onChange(Number(event.target.value))}
        onPointerUp={() => onEnd?.()}
        onPointerCancel={() => onEnd?.()}
        onBlur={() => onEnd?.()}
      />
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
  onStitch,
  onScrollCapture,
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
  onStitch: () => void;
  onScrollCapture: () => void;
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
        <button disabled={busy} onClick={onStitch}><MoveUpRight size={15} />长截图拼接…</button>
        <button disabled={busy} onClick={onScrollCapture}><ScanLine size={15} />滚动截图…</button>
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
  captureShortcut,
  onSettings,
}: {
  busy: boolean;
  onDrag: (event: React.PointerEvent<HTMLDivElement>) => void;
  onCapture: () => void;
  onOpen: () => void;
  onOpenProject: () => void;
  onClose: () => void;
  captureShortcut?: string;
  onSettings?: () => void;
}) {
  const displayShortcut = shortcutParts(captureShortcut || "CommandOrControl+Shift+A");
  return (
    <div className="empty-state" onPointerDown={onDrag}>
      {onSettings && (
        <button className="empty-settings" title="设置" aria-label="设置" onClick={onSettings}>
          <Settings size={15} />
        </button>
      )}
      <button className="empty-close" title="关闭到托盘" aria-label="关闭到托盘" onClick={onClose}><X size={15} /></button>
      <div className="empty-symbol"><Aperture size={26} /></div>
      <div className="empty-actions">
        <button className="primary-action large" disabled={busy} onClick={onCapture}><ScanLine size={16} />截取屏幕</button>
        <button className="secondary-action" onClick={onOpen}><ImagePlus size={16} />打开图片</button>
        <button className="secondary-action" onClick={onOpenProject}><FolderOpen size={16} />工程</button>
      </div>
      <div className="shortcut-row">
        {displayShortcut.map((key, index) => (
          <Fragment key={`${key}-${index}`}>
            {index > 0 && <span>+</span>}
            <kbd>{key}</kbd>
          </Fragment>
        ))}
      </div>
    </div>
  );
}
function FilePickers({
  imageInput,
  projectInput,
  onImage,
  onProject,
  onBeforeReplace,
  onError,
}: {
  imageInput: React.RefObject<HTMLInputElement | null>;
  projectInput: React.RefObject<HTMLInputElement | null>;
  onImage: (capture: CaptureResult, name?: string) => Promise<void>;
  onProject: (project: ScreenPinProject) => Promise<void>;
  onBeforeReplace: () => Promise<boolean>;
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
            if (!(await onBeforeReplace())) return;
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
            if (!(await onBeforeReplace())) return;
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

function StitchPanel({
  sources,
  direction,
  busy,
  onAddFiles,
  onDirection,
  onReorder,
  onRemove,
  onStitch,
  onClose,
}: {
  sources: CaptureResult[];
  direction: StitchDirection;
  busy: boolean;
  onAddFiles: () => void;
  onDirection: (direction: StitchDirection) => void;
  onReorder: (from: number, to: number) => void;
  onRemove: (index: number) => void;
  onStitch: () => void;
  onClose: () => void;
}) {
  return (
    <div className="pin-panel is-stitch" role="dialog" aria-label="长截图拼接">
      <div className="panel-title">
        <span className="ocr-title"><MoveUpRight size={14} />长截图拼接</span>
        <button className="panel-close" title="关闭" aria-label="关闭" onClick={onClose}><X size={13} /></button>
      </div>
      <div className="stitch-dir">
        <span className="panel-stitch-label">拼接方向</span>
        <div className="stitch-dir-options">
          <button
            type="button"
            className={direction === "vertical" ? "is-active" : ""}
            onClick={() => onDirection("vertical")}
          >
            垂直 <ArrowDown size={13} />
          </button>
          <button
            type="button"
            className={direction === "horizontal" ? "is-active" : ""}
            onClick={() => onDirection("horizontal")}
          >
            水平 <ArrowUp size={13} />
          </button>
        </div>
      </div>
      <div className="stitch-list">
        {sources.length === 0 ? (
          <div className="ocr-status">还没有图片，点击下方"添加图片"选择多张截图，可调整顺序后再拼接。</div>
        ) : (
          sources.map((source, index) => (
            <div key={`${index}-${source.dataUrl.slice(0, 24)}`} className="stitch-item">
              <span className="stitch-index">{index + 1}</span>
              <img className="stitch-thumb" src={source.dataUrl} alt={`第 ${index + 1} 张`} draggable={false} />
              <span className="stitch-size">{source.width} × {source.height}</span>
              <div className="stitch-item-actions">
                <button title="上移" aria-label="上移" disabled={index === 0} onClick={() => onReorder(index, index - 1)}><ArrowUp size={13} /></button>
                <button title="下移" aria-label="下移" disabled={index === sources.length - 1} onClick={() => onReorder(index, index + 1)}><ArrowDown size={13} /></button>
                <button title="移除" aria-label="移除" onClick={() => onRemove(index)}><Trash2 size={13} /></button>
              </div>
            </div>
          ))
        )}
      </div>
      <div className="panel-actions panel-actions-form">
        <button type="button" disabled={busy} onClick={onAddFiles}><ImagePlus size={14} />添加图片</button>
        <button type="button" className="primary-action" disabled={busy || sources.length < 2} onClick={onStitch}>
          <MoveUpRight size={14} />拼接并载入
        </button>
      </div>
    </div>
  );
}

function OcrPanel({
  busy,
  text,
  setupError,
  onRetry,
  onCopy,
  onClose,
}: {
  busy: boolean;
  text: string | null;
  setupError: string | null;
  onRetry: () => void;
  onCopy: () => void;
  onClose: () => void;
}) {
  const lineCount = text ? text.split("\n").filter((line) => line.trim().length > 0).length : 0;
  const charCount = text ? Array.from(text.replace(/\s+/g, "")).length : 0;
  return (
    <div className="pin-panel is-ocr" role="dialog" aria-label="识别文字">
      <div className="panel-title">
        <span className="ocr-title"><ScanText size={14} />识别文字 (OCR)</span>
        <button className="panel-close" title="关闭" aria-label="关闭" onClick={onClose}><X size={13} /></button>
      </div>
      {setupError ? (
        <div className="ocr-setup-error">{setupError}</div>
      ) : busy ? (
        <div className="ocr-loading">
          <span className="ocr-spinner" aria-hidden />
          <span>正在识别…</span>
        </div>
      ) : text !== null && text.length > 0 ? (
        <>
          <textarea
            className="ocr-text"
            readOnly
            value={text}
            onFocus={(event) => event.currentTarget.select()}
            aria-label="识别出的文字"
          />
          <div className="ocr-meta">{lineCount} 行 · {charCount} 字符</div>
        </>
      ) : (
        <div className="ocr-status">没有识别到文字，换个画面试试。</div>
      )}
      <div className="panel-actions panel-actions-form">
        {!setupError && text !== null && text.length > 0 && (
          <button type="button" className="primary-action" disabled={busy} onClick={onCopy}><Copy size={14} />复制文字</button>
        )}
        <button type="button" disabled={busy} onClick={onRetry}>重新识别</button>
        <button type="button" onClick={onClose}>关闭</button>
      </div>
    </div>
  );
}

function ShortcutsPanel({ exportFormat, captureShortcut }: { exportFormat: ExportFormat; captureShortcut?: string }) {
  const shortcuts = [
    { keys: shortcutParts(captureShortcut || "CommandOrControl+Shift+A"), action: "截取屏幕（全局）" },
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

function SettingsPanel({
  config,
  onSave,
  onClose,
}: {
  config: AppConfig;
  onSave: (config: AppConfig) => Promise<void>;
  onClose: () => void;
}) {
  const [shortcut, setShortcut] = useState(config.captureShortcut);
  const [isRecording, setIsRecording] = useState(false);
  const [isSaving, setIsSaving] = useState(false);
  const [pending, setPending] = useState<string[]>([]);
  const [hint, setHint] = useState<string | null>(null);

  // A capture-phase listener on the window, rather than React's onKeyDown on
  // the panel: the panel is not focusable, so modifier-only presses such as the
  // Win key would otherwise never reach it, and the editor's own global
  // shortcut handler would react to the combination being recorded.
  useEffect(() => {
    if (!isRecording) return;
    // Accumulate held modifiers from the pressed key's code rather than from
    // `event.metaKey`, which WebKit reports only inconsistently while the Win
    // key is down. A combination is only saved once a non-modifier key is
    // pressed while at least one modifier is being held.
    let held = new Set<string>();
    const finish = (key: string) => {
      const modifiers = MODIFIER_ORDER.filter((m) => held.has(m));
      setShortcut([...modifiers, key].join("+"));
      setPending([]);
      setHint(null);
      setIsRecording(false);
    };
    const onKeyDown = (event: KeyboardEvent) => {
      event.preventDefault();
      event.stopImmediatePropagation();
      const modifier = modifierFromEvent(event);
      if (modifier) {
        held.add(modifier);
        setPending(MODIFIER_ORDER.filter((m) => held.has(m)));
        setHint(null);
        return;
      }
      if (held.size === 0) {
        setPending([]);
        setHint("全局快捷键需要至少一个修饰键，例如 Ctrl 或 Win");
        return;
      }
      const key = mainShortcutKey(event);
      if (key === null) {
        setPending([]);
        setHint("不支持这个按键，请换一个");
        return;
      }
      finish(key);
    };
    const onKeyUp = (event: KeyboardEvent) => {
      event.preventDefault();
      event.stopImmediatePropagation();
      const modifier = modifierFromEvent(event);
      if (!modifier) return;
      held.delete(modifier);
      // Still holding other modifiers keeps the preview; if everything is
      // released without a main key, clear it so a swallowed Win press does not
      // look like it registered.
      if (held.size > 0) {
        setPending(MODIFIER_ORDER.filter((m) => held.has(m)));
      } else {
        setPending([]);
      }
    };
    window.addEventListener("keydown", onKeyDown, true);
    window.addEventListener("keyup", onKeyUp, true);
    return () => {
      window.removeEventListener("keydown", onKeyDown, true);
      window.removeEventListener("keyup", onKeyUp, true);
    };
  }, [isRecording]);

  const toggleRecording = () => {
    setPending([]);
    setHint(null);
    setIsRecording((current) => !current);
  };

  const saveShortcut = async () => {
    setIsSaving(true);
    try {
      await onSave({ captureShortcut: shortcut });
    } catch {
      // The parent displays the native registration error and leaves this
      // panel open so the user can choose another combination.
    } finally {
      setIsSaving(false);
    }
  };

  return (
    <div className="pin-panel is-settings" role="dialog" aria-label="设置">
      <div className="panel-title">
        <span>设置</span>
      </div>
      <label className="panel-field">
        <span>截图快捷键</span>
        <div className="shortcut-input-wrapper">
          <input
            className="shortcut-input"
            value={isRecording && pending.length > 0 ? `${formatShortcut(pending.join("+"))}+…` : formatShortcut(shortcut)}
            readOnly
            placeholder={isRecording ? "按下想要的组合键" : "点击录制按钮设置快捷键"}
          />
          <button className={isRecording ? "is-recording" : ""} onClick={toggleRecording}>
            {isRecording ? "按下快捷键..." : "录制"}
          </button>
        </div>
        <span className="panel-hint">
          {hint ?? (isRecording ? "Win 键可以作为修饰键；部分组合会被桌面环境占用" : "全局快捷键，在任何界面都可触发截图")}
        </span>
      </label>
      <div className="panel-actions panel-actions-form">
        <button type="button" disabled={isSaving} onClick={onClose}>取消</button>
        <button type="button" className="primary-action" disabled={isSaving || isRecording} onClick={() => void saveShortcut()}>
          {isSaving ? "正在注册..." : "保存"}
        </button>
      </div>
    </div>
  );
}
