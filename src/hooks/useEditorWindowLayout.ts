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
  clamp,
  roundZoom,
  zoomToFit,
} from "../editor/constants";
import { fitWindow, isTauri, workArea } from "../platform";
import type { ImageSource } from "../types";

interface UseEditorWindowLayoutOptions {
  image: ImageSource | null;
  zoom: number;
  setZoom: Dispatch<SetStateAction<number>>;
}

export function useEditorWindowLayout({ image, zoom, setZoom }: UseEditorWindowLayoutOptions) {
  const stageRef = useRef<HTMLDivElement>(null);
  const barRef = useRef<HTMLDivElement>(null);
  const [barHeight, setBarHeight] = useState(ASSUMED_BAR_HEIGHT);
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
    const request = fitWindow(size.width, size.height, center);
    pendingWindowFit.current = request;
    void request.catch(() => undefined);
  }, [barHeight, image, stage?.height, stage?.width]);

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
      setZoom((current) => roundZoom(clamp(current * (event.deltaY < 0 ? 1.1 : 1 / 1.1), MIN_ZOOM, MAX_ZOOM)));
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
    refreshArea,
    requestRecenter,
    stage,
    stageRef,
    waitForWindowFit,
    zoomToScreen,
  };
}
