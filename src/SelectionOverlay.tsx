import { useCallback, useEffect, useRef, useState, type PointerEvent } from "react";
import { invoke } from "@tauri-apps/api/core";
import { boxOf, clamp, shiftCorners, type Point, type Rect } from "./selectionGeometry";

interface OverlayInfo {
  session: string;
  imageWidth: number;
  imageHeight: number;
  crop: { x: number; y: number; width: number; height: number };
}

// Below this many CSS pixels a press is treated as a stray click, not a region.
const MIN_REGION = 4;

/**
 * One monitor's region picker. It shows this monitor's slice of the full
 * desktop capture; dragging selects, releasing confirms, Esc or right click
 * cancels, Enter takes the whole monitor.
 */
export function SelectionOverlay() {
  const [info, setInfo] = useState<OverlayInfo | null>(null);
  const [shot, setShot] = useState<string | null>(null);
  const [rect, setRect] = useState<Rect | null>(null);
  // The drag's fixed corner and its moving corner. Holding Space while
  // dragging shifts both, moving the box instead of resizing it.
  const origin = useRef<Point | null>(null);
  const corner = useRef<Point | null>(null);
  const pointer = useRef<Point | null>(null);
  const moving = useRef(false);
  const finished = useRef(false);

  useEffect(() => {
    let url: string | null = null;
    let cancelled = false;
    Promise.all([invoke<OverlayInfo>("overlay_info"), invoke<ArrayBuffer>("overlay_image")])
      .then(([overlay, bytes]) => {
        if (cancelled) return;
        url = URL.createObjectURL(new Blob([bytes], { type: "image/png" }));
        setInfo(overlay);
        setShot(url);
      })
      .catch(() => void invoke("cancel_selection"));
    return () => {
      cancelled = true;
      if (url) URL.revokeObjectURL(url);
    };
  }, []);

  const finish = useCallback((region: Rect | null) => {
    if (finished.current) return;
    finished.current = true;
    if (!region) {
      void invoke("cancel_selection");
      return;
    }
    void invoke("finish_selection", {
      region: { ...region, viewWidth: window.innerWidth, viewHeight: window.innerHeight },
    }).catch(() => void invoke("cancel_selection"));
  }, []);

  // Re-render once the window gets its real size (the size label reads it).
  const [, setViewport] = useState(0);
  useEffect(() => {
    const onResize = () => setViewport((count) => count + 1);
    window.addEventListener("resize", onResize);
    return () => window.removeEventListener("resize", onResize);
  }, []);

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") finish(null);
      if (event.key === "Enter") finish({ x: 0, y: 0, width: window.innerWidth, height: window.innerHeight });
      if (event.key === " ") {
        event.preventDefault();
        moving.current = true;
      }
    };
    const onKeyUp = (event: KeyboardEvent) => {
      if (event.key === " ") moving.current = false;
    };
    window.addEventListener("keydown", onKey);
    window.addEventListener("keyup", onKeyUp);
    return () => {
      window.removeEventListener("keydown", onKey);
      window.removeEventListener("keyup", onKeyUp);
    };
  }, [finish]);

  const onPointerDown = (event: PointerEvent<HTMLDivElement>) => {
    if (event.button === 2) {
      finish(null);
      return;
    }
    if (event.button !== 0) return;
    event.currentTarget.setPointerCapture(event.pointerId);
    void invoke("overlay_focus").catch(() => undefined);
    const point = { x: event.clientX, y: event.clientY };
    origin.current = point;
    corner.current = point;
    pointer.current = point;
    setRect({ ...point, width: 0, height: 0 });
  };

  const onPointerMove = (event: PointerEvent<HTMLDivElement>) => {
    const start = origin.current;
    const end = corner.current;
    if (!start || !end) return;
    const x = clamp(event.clientX, 0, window.innerWidth);
    const y = clamp(event.clientY, 0, window.innerHeight);
    const last = pointer.current ?? { x, y };
    pointer.current = { x, y };
    const [nextStart, nextEnd] = moving.current
      ? shiftCorners(start, end, x - last.x, y - last.y, { width: window.innerWidth, height: window.innerHeight })
      : [start, { x, y }];
    origin.current = nextStart;
    corner.current = nextEnd;
    setRect(boxOf(nextStart, nextEnd));
  };

  const onPointerUp = () => {
    if (!origin.current || !corner.current) return;
    const region = boxOf(origin.current, corner.current);
    origin.current = null;
    corner.current = null;
    pointer.current = null;
    if (region.width >= MIN_REGION && region.height >= MIN_REGION) finish(region);
    else setRect(null);
  };

  if (!info || !shot) return <div className="selection-overlay" />;

  // Size the full capture in percentages so this monitor's slice fills the
  // window. The page renders while the window is still hidden at 0x0, so
  // anything derived from innerWidth here would stay collapsed until the
  // next re-render.
  const percent = (value: number, total: number) => `${(value / total) * 100}%`;
  const scaleX = window.innerWidth / info.crop.width;
  const scaleY = window.innerHeight / info.crop.height;
  const pixels = rect
    ? `${Math.round(rect.width / scaleX)} × ${Math.round(rect.height / scaleY)}`
    : null;

  return (
    <div
      className="selection-overlay"
      onPointerDown={onPointerDown}
      onPointerMove={onPointerMove}
      onPointerUp={onPointerUp}
      onContextMenu={(event) => event.preventDefault()}
    >
      <img
        className="selection-shot"
        alt=""
        draggable={false}
        src={shot}
        style={{
          width: percent(info.imageWidth, info.crop.width),
          height: percent(info.imageHeight, info.crop.height),
          left: percent(-info.crop.x, info.crop.width),
          top: percent(-info.crop.y, info.crop.height),
        }}
        onLoad={() => void invoke("overlay_ready")}
        onError={() => void invoke("overlay_ready")}
      />
      {rect && rect.width > 0 && rect.height > 0 ? (
        <div className="selection-rect" style={{ left: rect.x, top: rect.y, width: rect.width, height: rect.height }}>
          <span className={`selection-size${rect.y < 28 ? " is-inside" : ""}`}>{pixels}</span>
        </div>
      ) : (
        <div className="selection-dim">
          <span className="selection-hint">拖动选择区域，松开截取 · 拖动时按住空格移动选区 · Enter 截取整屏 · Esc 取消</span>
        </div>
      )}
    </div>
  );
}

