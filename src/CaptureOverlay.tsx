import { useCallback, useEffect, useRef, useState } from "react";
import { Check, X } from "lucide-react";
import type { CaptureResult, Point, ScreenBounds } from "./types";
import { setCaptureTarget } from "./platform";

export interface CaptureSelection {
  x: number;
  y: number;
  width: number;
  height: number;
}

type ResizeHandle = "n" | "ne" | "e" | "se" | "s" | "sw" | "w" | "nw";
type DragState =
  | { kind: "create"; origin: Point }
  | { kind: "move"; origin: Point; initial: CaptureSelection }
  | { kind: "resize"; initial: CaptureSelection; handle: ResizeHandle };

const HANDLES: ResizeHandle[] = ["nw", "n", "ne", "e", "se", "s", "sw", "w"];

const clamp = (value: number, min: number, max: number) => Math.min(max, Math.max(min, value));

/**
 * The rectangle of the browser surface that actually shows the captured image.
 * The image is letterboxed (`object-fit: contain`) so a whole virtual desktop
 * stays fully visible even when the selector window covers a single monitor —
 * which is all Wayland lets a program do. Pointer and selection coordinates are
 * mapped through this rect in both directions.
 */
export function imageDisplayRect(
  surface: { left: number; top: number; width: number; height: number },
  image: { width: number; height: number },
): { left: number; top: number; width: number; height: number } {
  if (surface.width <= 0 || surface.height <= 0 || image.width <= 0 || image.height <= 0) {
    return { left: surface.left, top: surface.top, width: surface.width, height: surface.height };
  }
  const surfaceRatio = surface.width / surface.height;
  const imageRatio = image.width / image.height;
  if (imageRatio > surfaceRatio) {
    const width = surface.width;
    const height = surface.width / imageRatio;
    return { left: surface.left, top: surface.top + (surface.height - height) / 2, width, height };
  }
  const height = surface.height;
  const width = surface.height * imageRatio;
  return { left: surface.left + (surface.width - width) / 2, top: surface.top, width, height };
}

/** Convert an image-pixel point to CSS pixel coordinates on the surface. */
export function constainPoint(
  rect: { left: number; top: number; width: number; height: number },
  image: { width: number; height: number },
  x: number,
  y: number,
): { x: number; y: number } {
  return {
    x: rect.left + (x / image.width) * rect.width,
    y: rect.top + (y / image.height) * rect.height,
  };
}

/** Convert a CSS surface point to image-pixel coordinates. */
export function unconstainPoint(
  rect: { left: number; top: number; width: number; height: number },
  image: { width: number; height: number },
  x: number,
  y: number,
): { x: number; y: number } {
  if (rect.width === 0 || rect.height === 0) return { x: 0, y: 0 };
  return {
    x: ((x - rect.left) / rect.width) * image.width,
    y: ((y - rect.top) / rect.height) * image.height,
  };
}

/** The monitor whose bitmap rectangle contains an image-pixel point. */
export function screenAtPoint(screens: ScreenBounds[], point: Point): ScreenBounds | undefined {
  return screens.find(
    (s) => point.x >= s.x && point.x <= s.x + s.width && point.y >= s.y && point.y <= s.y + s.height,
  );
}

/** A monitor's rectangle in a normalized 0..1 layout space. */
export interface ScreenLayoutRect {
  screenIndex: number;
  x: number;
  y: number;
  width: number;
  height: number;
}

/**
 * True when screens are stacked vertically (their x-ranges overlap), which
 * decides whether the screen-picker lays them in a row or a column.
 */
export function screenLayoutAxis(screens: ScreenBounds[]): "row" | "column" {
  if (screens.length < 2) return "row";
  const primaryIndex = screens.findIndex((s) => s.primary);
  const anchorIndex = primaryIndex >= 0 ? primaryIndex : 0;
  const anchor = screens[anchorIndex];
  const overlap = screens.some((s, i) => {
    if (i === anchorIndex) return false;
    return s.x < anchor.x + anchor.width && anchor.x < s.x + s.width;
  });
  return overlap ? "column" : "row";
}

export function normalizeSelection(start: Point, end: Point): CaptureSelection {
  return {
    x: Math.min(start.x, end.x),
    y: Math.min(start.y, end.y),
    width: Math.abs(end.x - start.x),
    height: Math.abs(end.y - start.y),
  };
}

export function moveSelection(
  selection: CaptureSelection,
  delta: Point,
  bounds: { width: number; height: number },
): CaptureSelection {
  return {
    ...selection,
    x: clamp(selection.x + delta.x, 0, Math.max(0, bounds.width - selection.width)),
    y: clamp(selection.y + delta.y, 0, Math.max(0, bounds.height - selection.height)),
  };
}

export function resizeSelection(
  selection: CaptureSelection,
  handle: ResizeHandle,
  point: Point,
  bounds: { width: number; height: number },
): CaptureSelection {
  let left = selection.x;
  let top = selection.y;
  let right = selection.x + selection.width;
  let bottom = selection.y + selection.height;
  if (handle.includes("w")) left = clamp(point.x, 0, right - 1);
  if (handle.includes("e")) right = clamp(point.x, left + 1, bounds.width);
  if (handle.includes("n")) top = clamp(point.y, 0, bottom - 1);
  if (handle.includes("s")) bottom = clamp(point.y, top + 1, bounds.height);
  return { x: left, y: top, width: right - left, height: bottom - top };
}

export async function cropCapture(
  capture: CaptureResult,
  selection: CaptureSelection,
): Promise<CaptureResult> {
  const x = clamp(Math.round(selection.x), 0, capture.width - 1);
  const y = clamp(Math.round(selection.y), 0, capture.height - 1);
  const width = clamp(Math.round(selection.width), 1, capture.width - x);
  const height = clamp(Math.round(selection.height), 1, capture.height - y);
  const source = await loadImage(capture.dataUrl);
  if (source.naturalWidth !== capture.width || source.naturalHeight !== capture.height) {
    throw new Error("待选取图像的实际尺寸不一致");
  }
  const canvas = document.createElement("canvas");
  canvas.width = width;
  canvas.height = height;
  const context = canvas.getContext("2d");
  if (!context) throw new Error("浏览器画布不可用");
  context.drawImage(source, x, y, width, height, 0, 0, width, height);
  return { dataUrl: canvas.toDataURL("image/png"), width, height };
}

/**
 * Crop a real screen out of the shared whole-desktop bitmap and downscale it to
 * exactly `width × height` pixels, so the whole screen fits the box at native
 * sharpness (matched to the box size × devicePixelRatio). It is never upscaled,
 * and neighbouring screens never bleed in because only this monitor's region is
 * drawn.
 */
export async function cropThumb(
  capture: CaptureResult,
  screen: ScreenBounds,
  width: number,
  height: number,
): Promise<string> {
  const source = await loadImage(capture.dataUrl);
  const tw = Math.max(1, Math.round(width));
  const th = Math.max(1, Math.round(height));
  const canvas = document.createElement("canvas");
  canvas.width = tw;
  canvas.height = th;
  const context = canvas.getContext("2d");
  if (!context) return canvas.toDataURL("image/png");
  context.imageSmoothingEnabled = true;
  context.imageSmoothingQuality = "high";
  context.drawImage(source, screen.x, screen.y, screen.width, screen.height, 0, 0, tw, th);
  return canvas.toDataURL("image/png");
}

function loadImage(dataUrl: string): Promise<HTMLImageElement> {
  return new Promise((resolve, reject) => {
    const image = new Image();
    image.onload = () => resolve(image);
    image.onerror = () => reject(new Error("无法读取待选取的屏幕图像"));
    image.src = dataUrl;
  });
}

export function CaptureOverlay({
  capture,
  screens,
  onConfirm,
  onCancel,
}: {
  capture: CaptureResult;
  screens?: ScreenBounds[];
  onConfirm: (result: CaptureResult) => void;
  onCancel: () => void;
}) {
  const surfaceRef = useRef<HTMLDivElement>(null);
  const drag = useRef<DragState | null>(null);
  const [selection, setSelection] = useState<CaptureSelection | null>(null);
  const [locked, setLocked] = useState(false);
  // Multi-screen capture first asks which screen to take: show a scaled-down
  // preview of each monitor, let the user click one, then crop that screen
  // natively.
  const screenBounds = screens ?? [];
  const multiScreen = screenBounds.length > 1;
  const [screenImage, setScreenImage] = useState<CaptureResult | null>(null);
  const [thumbs, setThumbs] = useState<Record<number, string>>({});
  const active = screenImage ?? capture;
  const pickingScreen = multiScreen && !screenImage;

  // Downscale each screen to a whole-screen preview. Resolution is computed
  // purely from the layout constants + DPR (no DOM measurement, so it is
  // deterministic on first display and every re-pick), and generated × 2 so the
  // preview is never upscaled and stays sharp.
  useEffect(() => {
    if (!pickingScreen) return;
    let cancelled = false;
    const axis = screenLayoutAxis(screenBounds);
    const dpr = (typeof window !== "undefined" && window.devicePixelRatio) || 1;
    // Match the CSS thumbnail size (row: height-capped, column: width-capped).
    const viewH = typeof window !== "undefined" ? Math.min(window.innerHeight * 0.56, 360) : 360;
    const viewW = typeof window !== "undefined" ? Math.min(window.innerWidth * 0.76, 560) : 560;
    const build = async () => {
      const entries = await Promise.all(
        screenBounds.map(async (screen) => {
          const aspect = screen.height > 0 ? screen.width / screen.height : 16 / 9;
          const css =
            axis === "column"
              ? { width: viewW, height: viewW / aspect }
              : { width: viewH * aspect, height: viewH };
          const w = Math.max(2, Math.round(css.width * 2 * dpr));
          const h = Math.max(2, Math.round(css.height * 2 * dpr));
          const url = await cropThumb(capture, screen, w, h);
          return [screenBounds.indexOf(screen), url] as const;
        }),
      );
      if (!cancelled) {
        setThumbs(Object.fromEntries(entries.filter((e): e is [number, string] => Boolean(e))));
      }
    };
    void build();
    return () => {
      cancelled = true;
    };
  }, [pickingScreen, capture, screenBounds]);

  const currentRect = useCallback(() => {
    const bounds = surfaceRef.current?.getBoundingClientRect();
    return bounds
      ? imageDisplayRect(bounds, active)
      : { left: 0, top: 0, width: 0, height: 0 };
  }, [active]);

  const pointFromEvent = useCallback((event: { clientX: number; clientY: number }): Point => {
    const rect = currentRect();
    const point = unconstainPoint(rect, active, event.clientX, event.clientY);
    return {
      x: clamp(Math.round(point.x), 0, active.width),
      y: clamp(Math.round(point.y), 0, active.height),
    };
  }, [active, currentRect]);

  const confirm = useCallback(async () => {
    if (locked || !selection || selection.width < 2 || selection.height < 2) return;
    setLocked(true);
    try {
      const cropped = await cropCapture(active, selection);
      onConfirm(cropped);
    } catch {
      onCancel();
    }
  }, [active, locked, onCancel, onConfirm, selection]);

  const backToPicking = useCallback(() => {
    if (locked) return;
    setScreenImage(null);
    setSelection(null);
  }, [locked]);

  const cancel = useCallback(() => {
    if (locked) return;
    setLocked(true);
    onCancel();
  }, [locked, onCancel]);

  const pickScreen = useCallback(
    async (point: Point) => {
      const picked = screenAtPoint(screenBounds, point);
      if (!picked) return;
      setLocked(true);
      try {
        // Remember which monitor was picked so the editor window opens there.
        try {
          await setCaptureTarget(picked.originX, picked.originY);
        } catch {
          // Non-fatal: the editor simply stays on its current screen.
        }
        const frame = await cropCapture(capture, picked);
        setScreenImage(frame);
        setSelection(null);
      } finally {
        setLocked(false);
      }
    },
    [capture, screenBounds],
  );

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        event.preventDefault();
        cancel();
      } else if (event.key === "Enter" && selection) {
        event.preventDefault();
        void confirm();
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [cancel, confirm, selection]);

  const startCreate = (event: React.PointerEvent<HTMLDivElement>) => {
    if (event.button !== 0 || (event.target as HTMLElement).closest(".capture-selection, .capture-actions, .capture-screen")) return;
    // Screen-picking phase: clicking the empty area does nothing; the screen
    // buttons handle selection themselves.
    if (pickingScreen) return;
    drag.current = { kind: "create", origin: pointFromEvent(event) };
    setSelection({ x: drag.current.origin.x, y: drag.current.origin.y, width: 0, height: 0 });
    event.currentTarget.setPointerCapture(event.pointerId);
  };

  const updateDrag = (event: React.PointerEvent<HTMLDivElement>) => {
    const activeDrag = drag.current;
    if (!activeDrag) return;
    const point = pointFromEvent(event);
    if (activeDrag.kind === "create") {
      setSelection(normalizeSelection(activeDrag.origin, point));
    } else if (activeDrag.kind === "move") {
      setSelection(moveSelection(
        activeDrag.initial,
        { x: point.x - activeDrag.origin.x, y: point.y - activeDrag.origin.y },
        active,
      ));
    } else {
      setSelection(resizeSelection(activeDrag.initial, activeDrag.handle, point, active));
    }
  };

  const endDrag = (event: React.PointerEvent<HTMLDivElement>) => {
    if (!drag.current) return;
    drag.current = null;
    if (event.currentTarget.hasPointerCapture(event.pointerId)) {
      event.currentTarget.releasePointerCapture(event.pointerId);
    }
    setSelection((current) => current && current.width >= 2 && current.height >= 2 ? current : null);
  };

  const startMove = (event: React.PointerEvent<HTMLDivElement>) => {
    if (event.button !== 0 || !selection || (event.target as HTMLElement).classList.contains("capture-handle")) return;
    event.stopPropagation();
    drag.current = { kind: "move", origin: pointFromEvent(event), initial: selection };
    surfaceRef.current?.setPointerCapture(event.pointerId);
  };

  const startResize = (handle: ResizeHandle, event: React.PointerEvent<HTMLSpanElement>) => {
    if (event.button !== 0 || !selection) return;
    event.stopPropagation();
    drag.current = { kind: "resize", initial: selection, handle };
    surfaceRef.current?.setPointerCapture(event.pointerId);
  };

  const styleFor = (rect: CaptureSelection) => {
    const display = currentRect();
    const topLeft = constainPoint(display, active, rect.x, rect.y);
    const bottomRight = constainPoint(display, active, rect.x + rect.width, rect.y + rect.height);
    return {
      left: `${topLeft.x}px`,
      top: `${topLeft.y}px`,
      width: `${bottomRight.x - topLeft.x}px`,
      height: `${bottomRight.y - topLeft.y}px`,
    };
  };

  const shadeStyle = (edges: { top?: number; bottom?: number; left?: number; right?: number }) => {
    const display = currentRect();
    const p = (x: number, y: number) => constainPoint(display, active, x, y);
    return {
      top: edges.top === undefined ? undefined : `${p(0, edges.top).y}px`,
      height: edges.bottom === undefined ? undefined : `${p(0, edges.bottom).y - p(0, edges.top ?? 0).y}px`,
      left: edges.left === undefined ? undefined : `${p(edges.left, 0).x}px`,
      width: edges.right === undefined ? undefined : `${p(edges.right ?? 0, 0).x - p(edges.left ?? 0, 0).x}px`,
    };
  };

  const thumbLabel = (screen: ScreenBounds) => {
    const orientation = screen.width >= screen.height ? "横屏" : "竖屏";
    const parts = [`${screen.width}×${screen.height} · ${orientation}`];
    if (screen.cursorHere) parts.push("· 鼠标所在");
    if (screen.primary) parts.push("· 主屏");
    return parts.join(" ");
  };

  return (
    <div
      ref={surfaceRef}
      className="capture-overlay"
      role="dialog"
      aria-label="截图选区"
      onPointerDown={startCreate}
      onPointerMove={updateDrag}
      onPointerUp={endDrag}
      onPointerCancel={endDrag}
    >
      {!pickingScreen && <img className="capture-background" src={active.dataUrl} alt="待截取屏幕" draggable={false} />}
      {pickingScreen && (
        <div className={`capture-picker capture-picker-${screenLayoutAxis(screenBounds)}`}>
          {screenBounds.map((screen, index) => (
            <button
              key={`${screen.x}-${screen.y}`}
              className={`capture-screen${screen.cursorHere ? " is-cursor-here" : ""}`}
              onClick={() => void pickScreen({ x: screen.x + screen.width / 2, y: screen.y + screen.height / 2 })}
              aria-label={`屏幕 ${index + 1}：${thumbLabel(screen)}`}
            >
              {thumbs[index] ? (
                <img
                  className="capture-thumb"
                  src={thumbs[index]}
                  style={{ aspectRatio: `${screen.width} / ${screen.height}` }}
                  draggable={false}
                  alt=""
                />
              ) : (
                <span className="capture-thumb-placeholder" style={{ aspectRatio: `${screen.width} / ${screen.height}` }} />
              )}
              <span className="capture-screen-label">{thumbLabel(screen)}</span>
            </button>
          ))}
        </div>
      )}
      {!pickingScreen && selection ? (
        <>
          <div className="capture-shade capture-shade-top" style={shadeStyle({ bottom: selection.y })} />
          <div className="capture-shade capture-shade-left" style={shadeStyle({ top: selection.y, bottom: selection.y + selection.height, right: selection.x })} />
          <div className="capture-shade capture-shade-right" style={shadeStyle({ top: selection.y, bottom: selection.y + selection.height, left: selection.x + selection.width })} />
          <div className="capture-shade capture-shade-bottom" style={shadeStyle({ top: selection.y + selection.height })} />
          <div className="capture-selection" style={styleFor(selection)} onPointerDown={startMove} onDoubleClick={() => void confirm()}>
            <span className="capture-size">{selection.width} × {selection.height}</span>
            {HANDLES.map((handle) => (
              <span
                key={handle}
                className={`capture-handle capture-handle-${handle}`}
                onPointerDown={(event) => startResize(handle, event)}
              />
            ))}
          </div>
        </>
      ) : (
        !pickingScreen && <div className="capture-shade capture-shade-all" />
      )}
      <div className="capture-help">{pickingScreen ? "点击选择要截取的屏幕" : "拖动选择区域 · Enter 确认 · Esc 取消"}</div>
      <div className="capture-actions">
        {multiScreen && !pickingScreen && (
          <button disabled={locked} onClick={backToPicking} title="返回选择屏幕"><span aria-hidden>←</span>重新选屏</button>
        )}
        <button disabled={locked} onClick={cancel}><X size={16} />取消</button>
        <button className="is-primary" disabled={!selection || locked || pickingScreen} onClick={() => void confirm()}><Check size={16} />确认截图</button>
      </div>
    </div>
  );
}
