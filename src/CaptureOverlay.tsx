import { useCallback, useEffect, useRef, useState } from "react";
import { Check, X } from "lucide-react";
import type { CaptureResult, Point } from "./types";

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
  onConfirm,
  onCancel,
}: {
  capture: CaptureResult;
  onConfirm: (selection: CaptureSelection) => void;
  onCancel: () => void;
}) {
  const surfaceRef = useRef<HTMLDivElement>(null);
  const drag = useRef<DragState | null>(null);
  const [selection, setSelection] = useState<CaptureSelection | null>(null);
  const [locked, setLocked] = useState(false);

  const pointFromEvent = useCallback((event: { clientX: number; clientY: number }): Point => {
    const bounds = surfaceRef.current?.getBoundingClientRect();
    if (!bounds || bounds.width === 0 || bounds.height === 0) return { x: 0, y: 0 };
    return {
      x: clamp(Math.round(((event.clientX - bounds.left) / bounds.width) * capture.width), 0, capture.width),
      y: clamp(Math.round(((event.clientY - bounds.top) / bounds.height) * capture.height), 0, capture.height),
    };
  }, [capture.height, capture.width]);

  const confirm = useCallback(() => {
    if (locked || !selection || selection.width < 2 || selection.height < 2) return;
    setLocked(true);
    onConfirm(selection);
  }, [locked, onConfirm, selection]);

  const cancel = useCallback(() => {
    if (locked) return;
    setLocked(true);
    onCancel();
  }, [locked, onCancel]);

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        event.preventDefault();
        cancel();
      } else if (event.key === "Enter" && selection) {
        event.preventDefault();
        confirm();
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [cancel, confirm, selection]);

  const startCreate = (event: React.PointerEvent<HTMLDivElement>) => {
    if (event.button !== 0 || (event.target as HTMLElement).closest(".capture-selection, .capture-actions")) return;
    const origin = pointFromEvent(event);
    drag.current = { kind: "create", origin };
    setSelection({ x: origin.x, y: origin.y, width: 0, height: 0 });
    event.currentTarget.setPointerCapture(event.pointerId);
  };

  const updateDrag = (event: React.PointerEvent<HTMLDivElement>) => {
    const active = drag.current;
    if (!active) return;
    const point = pointFromEvent(event);
    if (active.kind === "create") {
      setSelection(normalizeSelection(active.origin, point));
    } else if (active.kind === "move") {
      setSelection(moveSelection(
        active.initial,
        { x: point.x - active.origin.x, y: point.y - active.origin.y },
        capture,
      ));
    } else {
      setSelection(resizeSelection(active.initial, active.handle, point, capture));
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

  const styleFor = (rect: CaptureSelection) => ({
    left: `${(rect.x / capture.width) * 100}%`,
    top: `${(rect.y / capture.height) * 100}%`,
    width: `${(rect.width / capture.width) * 100}%`,
    height: `${(rect.height / capture.height) * 100}%`,
  });

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
      <img className="capture-background" src={capture.dataUrl} alt="待截取屏幕" draggable={false} />
      {selection ? (
        <>
          <div className="capture-shade capture-shade-top" style={{ height: `${(selection.y / capture.height) * 100}%` }} />
          <div className="capture-shade capture-shade-left" style={{
            top: `${(selection.y / capture.height) * 100}%`,
            width: `${(selection.x / capture.width) * 100}%`,
            height: `${(selection.height / capture.height) * 100}%`,
          }} />
          <div className="capture-shade capture-shade-right" style={{
            top: `${(selection.y / capture.height) * 100}%`,
            left: `${((selection.x + selection.width) / capture.width) * 100}%`,
            height: `${(selection.height / capture.height) * 100}%`,
          }} />
          <div className="capture-shade capture-shade-bottom" style={{ top: `${((selection.y + selection.height) / capture.height) * 100}%` }} />
          <div className="capture-selection" style={styleFor(selection)} onPointerDown={startMove} onDoubleClick={confirm}>
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
        <div className="capture-shade capture-shade-all" />
      )}
      <div className="capture-help">拖动选择区域 · Enter 确认 · Esc 取消</div>
      <div className="capture-actions">
        <button disabled={locked} onClick={cancel}><X size={16} />取消</button>
        <button className="is-primary" disabled={!selection || locked} onClick={confirm}><Check size={16} />确认截图</button>
      </div>
    </div>
  );
}
