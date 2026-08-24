import { useCallback, useEffect, useRef, useState } from "react";
import { listen } from "@tauri-apps/api/event";
import { Play, Square, X } from "lucide-react";
import type { CaptureResult } from "./types";
import { captureRegion, setCapturePassthrough } from "./platform";
import { appendFrame, findScrollOffset, type ScrollDirection } from "./stitch";

export interface RegionRect {
  x: number;
  y: number;
  width: number;
  height: number;
}

interface DragState {
  origin: { x: number; y: number };
  current: { x: number; y: number };
}

/** Frame at the current captured region, in the overlay's own coordinate space. */
function readFrameImage(result: CaptureResult): Promise<ImageData> {
  return new Promise((resolve, reject) => {
    const image = new Image();
    image.onerror = () => reject(new Error("无法读取采集画面"));
    image.onload = () => {
      const canvas = document.createElement("canvas");
      canvas.width = result.width;
      canvas.height = result.height;
      const context = canvas.getContext("2d");
      if (!context) return reject(new Error("浏览器画布不可用"));
      context.drawImage(image, 0, 0);
      resolve(context.getImageData(0, 0, result.width, result.height));
    };
    image.src = result.dataUrl;
  });
}

/** Keep live preview encoding cheap as the assembled canvas grows. */
function previewDataUrl(source: HTMLCanvasElement): string {
  const scale = Math.min(1, 220 / source.width, 480 / source.height);
  const preview = document.createElement("canvas");
  preview.width = Math.max(1, Math.round(source.width * scale));
  preview.height = Math.max(1, Math.round(source.height * scale));
  preview.getContext("2d")?.drawImage(source, 0, 0, preview.width, preview.height);
  return preview.toDataURL("image/jpeg", 0.72);
}

/**
 * Fullscreen scroll-capture overlay: drag a region, then the app captures that
 * region repeatedly while the user scrolls, de-duplicating the overlap and
 * assembling a long image.
 */
export function ScrollCaptureOverlay({
  capture,
  onFinish,
  onCancel,
}: {
  capture: CaptureResult;
  onFinish: (result: CaptureResult) => void;
  onCancel: () => void;
}) {
  const surfaceRef = useRef<HTMLDivElement>(null);
  const drag = useRef<DragState | null>(null);
  const [selection, setSelection] = useState<RegionRect | null>(null);
  const [direction, setDirection] = useState<ScrollDirection>("vertical");
  const [capturing, setCapturing] = useState(false);
  const [stopping, setStopping] = useState(false);
  const [previewUrl, setPreviewUrl] = useState<string | null>(null);
  const [assembledSize, setAssembledSize] = useState({ width: 0, height: 0 });
  const [status, setStatus] = useState<string>("拖动框选要滚动截取的屏幕区域");

  // Live capture loop state kept in refs so the interval never re-subscribes.
  const activeRegion = useRef<RegionRect | null>(null);
  const previousFrame = useRef<ImageData | null>(null);
  const longCanvas = useRef<HTMLCanvasElement | null>(null);
  const busyRef = useRef(false);
  const cancelRef = useRef(false);
  const stoppingRef = useRef(false);
  const maxDurationRef = useRef<number | null>(null);
  const directionRef = useRef(direction);
  directionRef.current = direction;

  const pointFromEvent = useCallback((event: { clientX: number; clientY: number }): { x: number; y: number } => {
    // Logical (CSS) pixels relative to the window; the Rust side scales and
    // translates to desktop coordinates using the window's monitor.
    return { x: Math.round(event.clientX), y: Math.round(event.clientY) };
  }, []);

  const normalize = (origin: { x: number; y: number }, current: { x: number; y: number }): RegionRect => ({
    x: Math.min(origin.x, current.x),
    y: Math.min(origin.y, current.y),
    width: Math.abs(current.x - origin.x),
    height: Math.abs(current.y - origin.y),
  });

  const startDrag = (event: React.PointerEvent<HTMLDivElement>) => {
    if (capturing || event.button !== 0 || (event.target as HTMLElement).closest(".scroll-controls, .scroll-selection")) return;
    const origin = pointFromEvent(event);
    drag.current = { origin, current: origin };
    setSelection({ x: origin.x, y: origin.y, width: 0, height: 0 });
    event.currentTarget.setPointerCapture(event.pointerId);
  };

  const updateDrag = (event: React.PointerEvent<HTMLDivElement>) => {
    if (!drag.current) return;
    drag.current.current = pointFromEvent(event);
    setSelection(normalize(drag.current.origin, drag.current.current));
  };

  const endDrag = (event: React.PointerEvent<HTMLDivElement>) => {
    if (!drag.current) return;
    drag.current = null;
    if (event.currentTarget.hasPointerCapture(event.pointerId)) {
      event.currentTarget.releasePointerCapture(event.pointerId);
    }
    setSelection((current) => (current && current.width >= 8 && current.height >= 8 ? current : null));
  };

  const captureOnce = useCallback(async () => {
    const region = activeRegion.current;
    if (!region || busyRef.current || cancelRef.current) return;
    busyRef.current = true;
    try {
      const frame = await captureRegion(region.x, region.y, region.width, region.height);
      if (cancelRef.current) return;
      const imageData = await readFrameImage(frame);
      if (cancelRef.current) return;
      const canvas = longCanvas.current;
      if (!previousFrame.current) {
        // First frame seeds the long image.
        const seed = document.createElement("canvas");
        seed.width = imageData.width;
        seed.height = imageData.height;
        seed.getContext("2d")?.putImageData(imageData, 0, 0);
        longCanvas.current = seed;
        previousFrame.current = imageData;
        setAssembledSize({ width: imageData.width, height: imageData.height });
        setStatus("滚动页面，正在自动拼接…");
        setPreviewUrl(previewDataUrl(seed));
        return;
      }
      const match = findScrollOffset(previousFrame.current, imageData, directionRef.current);
      if (!match) {
        // No trustworthy overlap yet (content not scrolled or jumped too far).
        setStatus("等待画面滚动，请滚动页面…");
        return;
      }
      const nextCanvas = appendFrame(canvas!, imageData, directionRef.current, match.delta);
      longCanvas.current = nextCanvas;
      previousFrame.current = imageData;
      setAssembledSize({ width: nextCanvas.width, height: nextCanvas.height });
      setPreviewUrl(previewDataUrl(nextCanvas));
      const length = directionRef.current === "vertical" ? nextCanvas.height : nextCanvas.width;
      setStatus(`已拼接 ${length} 像素，继续滚动或按快捷键停止`);
    } catch (error) {
      cancelRef.current = true;
      setCapturing(false);
      void setCapturePassthrough(false).catch(() => undefined);
      setStatus(error instanceof Error ? error.message : String(error));
    } finally {
      busyRef.current = false;
    }
  }, []);

  useEffect(() => {
    if (!capturing) return;
    cancelRef.current = false;
    // Small initial delay so the hidden window settles, then poll steadily.
    const timeout = window.setTimeout(() => {
      void captureOnce();
      const interval = window.setInterval(() => void captureOnce(), 350);
      (intervalRef.current = interval);
    }, 250);
    return () => {
      window.clearTimeout(timeout);
      if (intervalRef.current) window.clearInterval(intervalRef.current);
      intervalRef.current = null;
      cancelRef.current = true;
    };
  }, [capturing, captureOnce]);

  const intervalRef = useRef<number | null>(null);

  const startCapture = async () => {
    if (!selection) return;
    try {
      await setCapturePassthrough(true);
    } catch (error) {
      setStatus(error instanceof Error ? error.message : String(error));
      return;
    }
    activeRegion.current = selection;
    previousFrame.current = null;
    longCanvas.current = null;
    setPreviewUrl(null);
    setAssembledSize({ width: 0, height: 0 });
    setStatus("请滚动目标页面，按 Esc 或再次按截图快捷键停止");
    setCapturing(true);
  };

  const stopCapture = async () => {
    if (stoppingRef.current) return;
    stoppingRef.current = true;
    setStopping(true);
    cancelRef.current = true;
    setCapturing(false);
    if (intervalRef.current) window.clearInterval(intervalRef.current);
    intervalRef.current = null;
    await setCapturePassthrough(false).catch(() => undefined);
    while (busyRef.current) {
      await new Promise<void>((resolve) => window.setTimeout(resolve, 20));
    }
    const canvas = longCanvas.current;
    if (canvas && canvas.width > 0 && canvas.height > 0) {
      const dataUrl = canvas.toDataURL("image/png");
      onFinish({ dataUrl, width: canvas.width, height: canvas.height });
    } else {
      onCancel();
    }
  };

  const cancel = () => {
    if (capturing) return;
    onCancel();
  };

  useEffect(() => {
    if (!capturing) return;
    maxDurationRef.current = window.setTimeout(() => {
      setStatus("滚动截图已达到 2 分钟上限，正在停止…");
      void stopCapture();
    }, 120_000);
    const unlisten = listen("capture-requested", () => void stopCapture());
    const unlistenStop = listen("scroll-capture-stop", () => void stopCapture());
    return () => {
      if (maxDurationRef.current) window.clearTimeout(maxDurationRef.current);
      maxDurationRef.current = null;
      void unlisten.then((dispose) => dispose());
      void unlistenStop.then((dispose) => dispose());
    };
    // stopCapture intentionally follows the latest capture state.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [capturing, stopping]);

  useEffect(() => () => {
    cancelRef.current = true;
    void setCapturePassthrough(false).catch(() => undefined);
  }, []);

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        event.preventDefault();
        if (capturing) {
          void stopCapture();
        } else {
          onCancel();
        }
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [capturing, stopping]);

  const styleFor = (rect: RegionRect) => {
    return {
      left: `${rect.x}px`,
      top: `${rect.y}px`,
      width: `${rect.width}px`,
      height: `${rect.height}px`,
    };
  };

  return (
    <div
      ref={surfaceRef}
      className={`capture-overlay scroll-overlay${capturing ? " is-capturing" : ""}`}
      role="dialog"
      aria-label="滚动截图"
      onPointerDown={startDrag}
      onPointerMove={updateDrag}
      onPointerUp={endDrag}
      onPointerCancel={endDrag}
    >
      {!capturing && <img className="scroll-background" src={capture.dataUrl} alt="待框选桌面" draggable={false} />}
      {!capturing && selection && (
        <div className="scroll-selection" style={styleFor(selection)} />
      )}

      {capturing && previewUrl && (
        <div className="scroll-preview">
          <img src={previewUrl} alt="长图预览" draggable={false} />
          <span className="scroll-preview-size">{assembledSize.width} × {assembledSize.height}</span>
        </div>
      )}

      <div className="scroll-controls">
        <span className="scroll-status">{status}</span>
        {!capturing && (
          <>
            <div className="scroll-dir">
              <button className={direction === "vertical" ? "is-active" : ""} onClick={() => setDirection("vertical")}>垂直</button>
              <button className={direction === "horizontal" ? "is-active" : ""} onClick={() => setDirection("horizontal")}>水平</button>
            </div>
            <button className="is-primary" disabled={!selection || selection.width < 8} onClick={() => void startCapture()}>
              <Play size={15} />开始滚动拼接
            </button>
            <button onClick={cancel}><X size={15} />取消</button>
          </>
        )}
        {capturing && (
          <span className="scroll-stop-hint"><Square size={13} />Esc / 截图快捷键停止</span>
        )}
      </div>

      {!capturing && (
        <div className="capture-help">拖动框选要截取的屏幕区域 · 可切换垂直/水平 · Esc 取消</div>
      )}
    </div>
  );
}
