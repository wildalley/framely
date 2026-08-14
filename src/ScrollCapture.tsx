import { useCallback, useEffect, useRef, useState } from "react";
import { Check, MoveUpRight, Play, Square, X } from "lucide-react";
import type { CaptureResult } from "./types";
import { captureRegion } from "./platform";
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

const clamp = (value: number, min: number, max: number) => Math.min(max, Math.max(min, value));

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

/**
 * Fullscreen scroll-capture overlay: drag a region, then the app captures that
 * region repeatedly while the user scrolls, de-duplicating the overlap and
 * assembling a long image.
 */
export function ScrollCaptureOverlay({
  onFinish,
  onCancel,
}: {
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
  const [height, setHeight] = useState(0);
  const [status, setStatus] = useState<string>("拖动框选要滚动截取的屏幕区域");

  // Live capture loop state kept in refs so the interval never re-subscribes.
  const activeRegion = useRef<RegionRect | null>(null);
  const previousFrame = useRef<ImageData | null>(null);
  const longCanvas = useRef<HTMLCanvasElement | null>(null);
  const busyRef = useRef(false);
  const cancelRef = useRef(false);
  const directionRef = useRef(direction);
  directionRef.current = direction;

  const pointFromEvent = useCallback((event: { clientX: number; clientY: number }): { x: number; y: number } => {
    const dpr = window.devicePixelRatio || 1;
    return { x: Math.round(event.clientX * dpr), y: Math.round(event.clientY * dpr) };
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
      const imageData = await readFrameImage(frame);
      const canvas = longCanvas.current;
      if (!previousFrame.current) {
        // First frame seeds the long image.
        const seed = document.createElement("canvas");
        seed.width = imageData.width;
        seed.height = imageData.height;
        seed.getContext("2d")?.putImageData(imageData, 0, 0);
        longCanvas.current = seed;
        previousFrame.current = imageData;
        setHeight(imageData.height);
        setStatus("滚动页面，正在自动拼接…");
        setPreviewUrl(seed.toDataURL("image/png"));
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
      setHeight(nextCanvas.height);
      setPreviewUrl(nextCanvas.toDataURL("image/png"));
      setStatus(`已拼接 ${nextCanvas.height} 像素，继续滚动或点击停止`);
    } catch (error) {
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

  const startCapture = () => {
    if (!selection) return;
    activeRegion.current = selection;
    previousFrame.current = null;
    longCanvas.current = null;
    setPreviewUrl(null);
    setHeight(0);
    setCapturing(true);
  };

  const stopCapture = async () => {
    if (stopping) return;
    setStopping(true);
    setCapturing(false);
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
    const dpr = window.devicePixelRatio || 1;
    return {
      left: `${rect.x / dpr}px`,
      top: `${rect.y / dpr}px`,
      width: `${rect.width / dpr}px`,
      height: `${rect.height / dpr}px`,
    };
  };

  return (
    <div
      ref={surfaceRef}
      className="capture-overlay scroll-overlay"
      role="dialog"
      aria-label="滚动截图"
      onPointerDown={startDrag}
      onPointerMove={updateDrag}
      onPointerUp={endDrag}
      onPointerCancel={endDrag}
    >
      {!capturing && selection && (
        <div className="scroll-selection" style={styleFor(selection)} />
      )}

      {capturing && previewUrl && (
        <div className="scroll-preview">
          <img src={previewUrl} alt="长图预览" draggable={false} />
          <span className="scroll-preview-size">{height}px</span>
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
            <button className="is-primary" disabled={!selection || selection.width < 8} onClick={startCapture}>
              <Play size={15} />开始滚动拼接
            </button>
            <button onClick={cancel}><X size={15} />取消</button>
          </>
        )}
        {capturing && (
          <button className="is-primary" disabled={stopping} onClick={() => void stopCapture()}>
            <Square size={15} />停止 ({height}px)
          </button>
        )}
      </div>

      {!capturing && (
        <div className="capture-help">拖动框选要截取的屏幕区域 · 可切换垂直/水平 · Esc 取消</div>
      )}
    </div>
  );
}
