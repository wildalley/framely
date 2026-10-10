import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type Dispatch,
  type SetStateAction,
} from "react";
import {
  ASSUMED_BAR_HEIGHT,
  EMPTY_WINDOW,
  MAX_ZOOM,
  MIN_WINDOW_WIDTH,
  MIN_ZOOM,
  PANEL_WIDTH,
  clamp,
  type EditorPanel,
  roundZoom,
  zoomToFit,
} from "../editor/constants";
import { fitWindow, isTauri, workArea } from "../platform";
import type { ImageSource } from "../types";

interface UseEditorWindowLayoutOptions {
  image: ImageSource | null;
  zoom: number;
  setZoom: Dispatch<SetStateAction<number>>;
  panel: EditorPanel;
}

// Gap between an open popover and the window edges (see `.pin-panel`).
const PANEL_MARGIN = 16;

export function useEditorWindowLayout({ image, zoom, setZoom, panel }: UseEditorWindowLayoutOptions) {
  const stageRef = useRef<HTMLDivElement>(null);
  const barRef = useRef<HTMLDivElement>(null);
  const [barHeight, setBarHeight] = useState(ASSUMED_BAR_HEIGHT);
  const [viewport, setViewport] = useState(() => ({ width: window.innerWidth, height: window.innerHeight }));
  // Natural height of the open popover, so a small screenshot does not crush it.
  const [panelHeight, setPanelHeight] = useState(0);
  const area = useRef<{ width: number; height: number } | null>(null);
  const barHeightRef = useRef(barHeight);
  const recenter = useRef(true);
  const pendingWindowFit = useRef<Promise<void> | null>(null);

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

  const requestedStage = image
    ? { width: Math.round(image.width * zoom), height: Math.round(image.height * zoom) }
    : null;
  // A window manager may constrain the native size independently of our fit
  // request. Fit both axes to the actual webview, preserving the image ratio.
  const displayZoom = image
    ? Math.min(zoom, Math.max(1, viewport.width) / image.width, Math.max(1, viewport.height - barHeight) / image.height)
    : zoom;
  const stage = image
    ? { width: image.width * displayZoom, height: image.height * displayZoom }
    : null;
  // Zoom controls step from what is on screen, not from a request the window
  // could not honour, so the shown percentage and each step stay truthful.
  const displayZoomRef = useRef(displayZoom);
  displayZoomRef.current = displayZoom;

  useLayoutEffect(() => {
    const update = () => setViewport({ width: window.innerWidth, height: window.innerHeight });
    window.addEventListener("resize", update);
    update();
    return () => window.removeEventListener("resize", update);
  }, []);

  useLayoutEffect(() => {
    barHeightRef.current = barHeight;
    if (!isTauri()) return;
    const center = recenter.current;
    recenter.current = false;
    const panelWidth = image && panel !== "none" ? PANEL_WIDTH[panel] + PANEL_MARGIN : 0;
    const panelSpace = image && panel !== "none" && panelHeight > 0 ? panelHeight + PANEL_MARGIN : 0;
    const size = requestedStage
      ? {
          width: Math.max(MIN_WINDOW_WIDTH, requestedStage.width, panelWidth),
          height: Math.max(requestedStage.height, panelSpace) + barHeight,
        }
      : EMPTY_WINDOW;
    const request = fitWindow(size.width, size.height, center);
    pendingWindowFit.current = request;
    void request.catch(() => undefined);
  }, [barHeight, image, panel, panelHeight, requestedStage?.height, requestedStage?.width]);

  useLayoutEffect(() => {
    if (panel === "none") {
      setPanelHeight(0);
      return;
    }
    const node = document.querySelector<HTMLElement>(".pin-panel");
    if (!node) return;
    const measure = () => {
      // scrollHeight is the content height even while max-height clips it.
      const next = Math.ceil(node.scrollHeight + node.offsetHeight - node.clientHeight);
      setPanelHeight((current) => (current === next ? current : next));
    };
    const observer = new ResizeObserver(measure);
    observer.observe(node);
    measure();
    return () => observer.disconnect();
  }, [image, panel]);

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

  const applyZoom = useCallback(
    (next: number) => setZoom(roundZoom(clamp(next, MIN_ZOOM, MAX_ZOOM))),
    [setZoom],
  );

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
      const current = displayZoomRef.current;
      setZoom(roundZoom(clamp(current * (event.deltaY < 0 ? 1.1 : 1 / 1.1), MIN_ZOOM, MAX_ZOOM)));
    };
    stageNode.addEventListener("wheel", onWheel, { passive: false });
    return () => stageNode.removeEventListener("wheel", onWheel);
  }, [image, setZoom]);

  const requestRecenter = useCallback(() => {
    recenter.current = true;
  }, []);

  const waitForWindowFit = useCallback(async () => {
    await pendingWindowFit.current?.catch(() => undefined);
  }, []);

  return {
    applyZoom,
    barHeight,
    barHeightRef,
    barRef,
    displayZoom,
    refreshArea,
    requestRecenter,
    stage,
    stageRef,
    waitForWindowFit,
    zoomToScreen,
  };
}
