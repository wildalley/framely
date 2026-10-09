import { forwardRef, memo, useCallback, useEffect, useImperativeHandle, useMemo, useRef, useState } from "react";
import type { MouseEvent as ReactMouseEvent, PointerEvent as ReactPointerEvent } from "react";
import type { Annotation, AnnotationStyle, ExportFormat, ImageSource, Point, Tool } from "./types";
import { createAnnotation, mimeForFormat, normalizeBox } from "./types";

export interface EditorCanvasHandle {
  exportPng: () => Promise<string>;
  exportImage: (format: ExportFormat, quality: number) => Promise<string>;
}

interface EditorCanvasProps {
  image: ImageSource;
  pixelatedImage: string | null;
  annotations: Annotation[];
  selectedId: string | null;
  tool: Tool;
  zoom: number;
  /** Style applied to the next annotation drawn with the active tool. */
  style: AnnotationStyle;
  onSelect: (id: string | null) => void;
  onToolChange: (tool: Tool) => void;
  onBegin: () => void;
  onTransientChange: (annotations: Annotation[]) => void;
  onEnd: (annotations: Annotation[]) => void;
}

type ResizeHandle = "nw" | "ne" | "sw" | "se" | "start" | "end";

type Interaction =
  | { kind: "draw"; id: string; start: Point }
  | { kind: "move"; id: string; start: Point; original: Annotation }
  | { kind: "resize"; id: string; start: Point; original: Annotation; handle: ResizeHandle };

const MIN_SIZE = 8;

export const EditorCanvas = forwardRef<EditorCanvasHandle, EditorCanvasProps>(function EditorCanvas(
  {
    image,
    pixelatedImage,
    annotations,
    selectedId,
    tool,
    zoom,
    style,
    onSelect,
    onToolChange,
    onBegin,
    onTransientChange,
    onEnd,
  },
  ref,
) {
  const svgRef = useRef<SVGSVGElement>(null);
  const interaction = useRef<Interaction | null>(null);
  const liveAnnotations = useRef(annotations);
  const textEditorRef = useRef<HTMLTextAreaElement>(null);
  const [editingTextId, setEditingTextId] = useState<string | null>(null);
  const [editingTextValue, setEditingTextValue] = useState("");
  liveAnnotations.current = annotations;

  const selected = useMemo(
    () => annotations.find((annotation) => annotation.id === selectedId) ?? null,
    [annotations, selectedId],
  );

  // One pass instead of a `.some()` plus three `.filter()` sweeps per render;
  // this runs on every pointermove while drawing.
  const { areaHighlights, mosaics, dimmerOpacity } = useMemo(() => {
    const areas: Annotation[] = [];
    const mosaicList: Annotation[] = [];
    let opacity = 0;
    for (const annotation of annotations) {
      if (annotation.type === "area-highlight") {
        areas.push(annotation);
        opacity = Math.max(opacity, annotation.opacity);
      } else if (annotation.type === "mosaic") {
        mosaicList.push(annotation);
      }
    }
    return { areaHighlights: areas, mosaics: mosaicList, dimmerOpacity: opacity };
  }, [annotations]);

  const renderToDataUrl = async (format: ExportFormat, quality: number) => {
    const svg = svgRef.current;
    if (!svg) throw new Error("编辑画布尚未就绪");
    if (annotations.some((annotation) => annotation.type === "mosaic") && !pixelatedImage) {
      throw new Error("马赛克处理失败，已阻止导出以保护原图");
    }
    const clone = svg.cloneNode(true) as SVGSVGElement;
    clone.querySelectorAll(".editor-selection").forEach((node) => node.remove());
    clone.querySelectorAll(".editor-text-editor").forEach((node) => node.remove());
    clone.setAttribute("xmlns", "http://www.w3.org/2000/svg");
    clone.setAttribute("width", String(image.width));
    clone.setAttribute("height", String(image.height));
    const blob = new Blob([new XMLSerializer().serializeToString(clone)], { type: "image/svg+xml" });
    const url = URL.createObjectURL(blob);
    try {
      const rendered = await loadImage(url);
      const canvas = document.createElement("canvas");
      canvas.width = image.width;
      canvas.height = image.height;
      const context = canvas.getContext("2d");
      if (!context) throw new Error("浏览器画布不可用");
      // JPEG has no alpha channel, so transparent pixels would turn black.
      if (format === "jpeg") {
        context.fillStyle = "#ffffff";
        context.fillRect(0, 0, canvas.width, canvas.height);
      }
      context.drawImage(rendered, 0, 0);
      const mime = mimeForFormat(format);
      const dataUrl = canvas.toDataURL(mime, quality);
      // Browsers silently fall back to PNG for formats they cannot encode.
      if (format !== "png" && !dataUrl.startsWith(`data:${mime}`)) {
        throw new Error(`当前系统不支持导出 ${format.toUpperCase()}`);
      }
      return dataUrl;
    } finally {
      URL.revokeObjectURL(url);
    }
  };

  useImperativeHandle(ref, () => ({
    exportPng: () => renderToDataUrl("png", 1),
    exportImage: (format: ExportFormat, quality: number) => renderToDataUrl(format, quality),
  }));

  // Reading the geometry per pointermove forces a synchronous layout flush on
  // every event, which is most of why dragging stutters on a high-polling-rate
  // mouse. It cannot change mid-drag, so measure it once when the drag begins.
  const dragMatrix = useRef<DOMMatrix | null>(null);
  const pendingPoints = useRef<Point[]>([]);
  const frame = useRef<number | null>(null);

  useEffect(() => () => {
    if (frame.current !== null) cancelAnimationFrame(frame.current);
  }, []);

  /**
   * The stage can be shorter than the image asks for — it is `flex: 0 1 auto`,
   * so a compositor that dictates the window size shrinks it — and the default
   * `preserveAspectRatio` then scales the viewBox down and centres it inside the
   * box. Dividing by the box size ignores that scale and offset, which is why
   * annotations landed away from the cursor. The SVG's own inverse screen matrix
   * accounts for it exactly, whatever the layout does.
   */
  const clientToImage = useCallback((clientX: number, clientY: number): Point => {
    const matrix = dragMatrix.current ?? svgRef.current?.getScreenCTM()?.inverse();
    if (!matrix) return { x: 0, y: 0 };
    const point = new DOMPoint(clientX, clientY).matrixTransform(matrix);
    return { x: point.x, y: point.y };
  }, []);

  const pointFromEvent = useCallback(
    (event: ReactPointerEvent<SVGElement>): Point => clientToImage(event.clientX, event.clientY),
    [clientToImage],
  );

  /** Pins the mapping used for this drag, then routes events to us until pointerup. */
  const beginPointerTracking = useCallback((event: ReactPointerEvent<SVGElement>) => {
    dragMatrix.current = svgRef.current?.getScreenCTM()?.inverse() ?? null;
    event.currentTarget.setPointerCapture(event.pointerId);
  }, []);

  const updateOne = useCallback(
    (id: string, update: (annotation: Annotation) => Annotation) => {
      const next = liveAnnotations.current.map((item) => (item.id === id ? update(item) : item));
      liveAnnotations.current = next;
      onTransientChange(next);
    },
    [onTransientChange],
  );

  const handleCanvasPointerDown = (event: ReactPointerEvent<SVGSVGElement>) => {
    if (event.button !== 0) return;
    if (tool === "select") {
      onSelect(null);
      return;
    }
    if (tool === "mosaic" && !pixelatedImage) return;
    beginPointerTracking(event);
    const start = pointFromEvent(event);
    const annotation = createAnnotation(tool, start, style);
    if (tool === "text") {
      annotation.width = 180;
      annotation.height = annotation.fontSize * 1.5;
    }
    onBegin();
    const next = [...annotations, annotation];
    liveAnnotations.current = next;
    onTransientChange(next);
    // Shapes stay selected so they can be adjusted right away. Freehand strokes
    // are drawn in runs: a selection box would flash around every stroke, and
    // the style panel would then restyle the last stroke instead of the next.
    onSelect(tool === "pen" || tool === "highlighter" ? null : annotation.id);
    interaction.current = { kind: "draw", id: annotation.id, start };
  };

  const handleObjectPointerDown = useCallback(
    (event: ReactPointerEvent<SVGElement>, annotation: Annotation) => {
      if (tool !== "select" || event.button !== 0) return;
      event.stopPropagation();
      beginPointerTracking(event);
      const start = pointFromEvent(event);
      onSelect(annotation.id);
      onBegin();
      interaction.current = { kind: "move", id: annotation.id, start, original: annotation };
    },
    [beginPointerTracking, onBegin, onSelect, pointFromEvent, tool],
  );

  const startTextEditing = useCallback(
    (annotation: Annotation) => {
      if (annotation.type !== "text") return;
      onSelect(annotation.id);
      onBegin();
      setEditingTextId(annotation.id);
      setEditingTextValue(annotation.text ?? "");
    },
    [onBegin, onSelect],
  );

  const handleObjectDoubleClick = useCallback(
    (event: ReactMouseEvent<SVGElement>, annotation: Annotation) => {
      if (annotation.type !== "text") return;
      event.stopPropagation();
      startTextEditing(annotation);
    },
    [startTextEditing],
  );

  const finishTextEditing = () => {
    if (!editingTextId) return;
    const id = editingTextId;
    setEditingTextId(null);
    onEnd(liveAnnotations.current);
    onSelect(id);
  };

  useEffect(() => {
    if (editingTextId) {
      textEditorRef.current?.focus();
      textEditorRef.current?.select();
    }
  }, [editingTextId]);

  const handleResizePointerDown = (
    event: ReactPointerEvent<SVGElement>,
    annotation: Annotation,
    handle: ResizeHandle,
  ) => {
    event.stopPropagation();
    beginPointerTracking(event);
    onBegin();
    interaction.current = {
      kind: "resize",
      id: annotation.id,
      start: pointFromEvent(event),
      original: annotation,
      handle,
    };
  };

  const applyPoints = useCallback(
    (points: Point[]) => {
      const active = interaction.current;
      const point = points.at(-1);
      if (!active || !point) return;
      if (active.kind === "draw") {
        updateOne(active.id, (annotation) => {
          if (annotation.type === "pen" || annotation.type === "highlighter") {
            return { ...annotation, points: [...(annotation.points ?? []), ...points] };
          }
          if (annotation.type === "arrow") {
            return { ...annotation, endX: point.x, endY: point.y, width: point.x - active.start.x, height: point.y - active.start.y };
          }
          if (annotation.type === "text") return annotation;
          return { ...annotation, width: point.x - active.start.x, height: point.y - active.start.y };
        });
        return;
      }
      const dx = point.x - active.start.x;
      const dy = point.y - active.start.y;
      if (active.kind === "move") {
        updateOne(active.id, () => moveAnnotation(active.original, dx, dy));
        return;
      }
      updateOne(active.id, () => resizeAnnotation(active.original, active.handle, dx, dy));
    },
    [updateOne],
  );

  const flushPendingPoints = useCallback(() => {
    frame.current = null;
    const points = pendingPoints.current;
    pendingPoints.current = [];
    applyPoints(points);
  }, [applyPoints]);

  // A mouse can report several hundred pointermove events per second, and each
  // one used to re-render the whole SVG. Buffer them and repaint once per frame
  // instead; freehand strokes keep every sampled point, so nothing is lost.
  const handlePointerMove = (event: ReactPointerEvent<SVGSVGElement>) => {
    if (!interaction.current) return;
    pendingPoints.current.push(pointFromEvent(event));
    if (frame.current === null) frame.current = requestAnimationFrame(flushPendingPoints);
  };

  const finishInteraction = () => {
    if (!interaction.current) return;
    if (frame.current !== null) {
      cancelAnimationFrame(frame.current);
      frame.current = null;
    }
    // Land the last buffered move before committing, so releasing the mouse
    // mid-frame cannot drop the final few pixels of the drag.
    const trailing = pendingPoints.current;
    pendingPoints.current = [];
    if (trailing.length > 0) applyPoints(trailing);
    dragMatrix.current = null;
    const active = interaction.current;
    interaction.current = null;
    let next = liveAnnotations.current.map((annotation) => {
      if (annotation.id !== active.id) return annotation;
      if (["rectangle", "ellipse", "area-highlight", "mosaic"].includes(annotation.type)) return normalizeBox(annotation);
      return annotation;
    });
    const created = next.find((annotation) => annotation.id === active.id);
    if (
      active.kind === "draw" &&
      created &&
      created.type !== "text" &&
      Math.abs(created.width) < MIN_SIZE &&
      Math.abs(created.height) < MIN_SIZE &&
      (created.points?.length ?? 0) < 2
    ) {
      next = next.filter((annotation) => annotation.id !== active.id);
      onSelect(null);
    }
    liveAnnotations.current = next;
    onTransientChange(next);
    onEnd(next);
    if (active.kind === "draw" && created?.type === "text") {
      onToolChange("select");
      window.setTimeout(() => startTextEditing(created), 0);
    }
  };

  return (
    <svg
      ref={svgRef}
      className={`editor-canvas tool-${tool}`}
      viewBox={`0 0 ${image.width} ${image.height}`}
      role="img"
      aria-label="截图标注画布"
      onPointerDown={handleCanvasPointerDown}
      onPointerMove={handlePointerMove}
      onPointerUp={finishInteraction}
      onPointerCancel={finishInteraction}
    >
      <defs>
        {areaHighlights.length > 0 && (
          <mask id="area-highlight-mask" maskUnits="userSpaceOnUse" x="0" y="0" width={image.width} height={image.height}>
            <rect width={image.width} height={image.height} fill="white" />
            {areaHighlights.map((annotation) => (
              <rect
                key={annotation.id}
                x={annotation.x}
                y={annotation.y}
                width={annotation.width}
                height={annotation.height}
                rx="3"
                fill="black"
              />
            ))}
          </mask>
        )}
        {mosaics.map((annotation) => (
          <clipPath id={`clip-${annotation.id}`} key={`clip-${annotation.id}`}>
            <rect x={annotation.x} y={annotation.y} width={annotation.width} height={annotation.height} rx="3" />
          </clipPath>
        ))}
      </defs>
      <image href={image.dataUrl} width={image.width} height={image.height} preserveAspectRatio="none" />
      {annotations.map((annotation) => (
        <AnnotationElement
          key={annotation.id}
          annotation={annotation}
          image={image}
          pixelatedImage={pixelatedImage}
          selected={annotation.id === selectedId}
          onPointerDown={handleObjectPointerDown}
          onDoubleClick={handleObjectDoubleClick}
        />
      ))}
      {areaHighlights.length > 0 && (
        <rect
          className="area-highlight-dimmer"
          width={image.width}
          height={image.height}
          fill="#000000"
          opacity={dimmerOpacity}
          mask="url(#area-highlight-mask)"
          pointerEvents="none"
        />
      )}
      {editingTextId && (
        <TextEditor
          ref={textEditorRef}
          annotation={annotations.find((annotation) => annotation.id === editingTextId) ?? null}
          value={editingTextValue}
          onChange={(value) => {
            setEditingTextValue(value);
            updateOne(editingTextId, (annotation) => ({ ...annotation, text: value }));
          }}
          onFinish={finishTextEditing}
        />
      )}
      {selected && (
        <Selection
          annotation={selected}
          zoom={zoom}
          onResizePointerDown={handleResizePointerDown}
        />
      )}
    </svg>
  );
});

const TextEditor = forwardRef<HTMLTextAreaElement, {
  annotation: Annotation | null;
  value: string;
  onChange: (value: string) => void;
  onFinish: () => void;
}>(function TextEditor({ annotation, value, onChange, onFinish }, ref) {
  if (!annotation || annotation.type !== "text") return null;
  return (
    <foreignObject
      className="editor-text-editor"
      x={annotation.x}
      y={annotation.y}
      width={Math.max(180, annotation.width)}
      height={Math.max(48, annotation.height)}
      style={{
        color: annotation.color,
        fontFamily: "Inter, 'Noto Sans CJK SC', sans-serif",
        fontSize: annotation.fontSize,
        fontWeight: 600,
      }}
      onPointerDown={(event) => event.stopPropagation()}
    >
      <textarea
        ref={ref}
        value={value}
        onChange={(event) => onChange(event.target.value)}
        onBlur={onFinish}
        onKeyDown={(event) => {
          if (event.key === "Escape" || (event.key === "Enter" && (event.ctrlKey || event.metaKey))) {
            event.preventDefault();
            event.currentTarget.blur();
          }
        }}
      />
    </foreignObject>
  );
});

/** Memoized so dragging one annotation does not re-render every other one. */
const AnnotationElement = memo(function AnnotationElement({
  annotation,
  image,
  pixelatedImage,
  selected,
  onPointerDown,
  onDoubleClick,
}: {
  annotation: Annotation;
  image: ImageSource;
  pixelatedImage: string | null;
  selected: boolean;
  onPointerDown: (event: ReactPointerEvent<SVGElement>, annotation: Annotation) => void;
  onDoubleClick: (event: ReactMouseEvent<SVGElement>, annotation: Annotation) => void;
}) {
  const common = {
    opacity: annotation.opacity,
    onPointerDown: (event: ReactPointerEvent<SVGElement>) => onPointerDown(event, annotation),
    onDoubleClick: (event: ReactMouseEvent<SVGElement>) => onDoubleClick(event, annotation),
    className: selected ? "annotation is-selected" : "annotation",
  };
  switch (annotation.type) {
    case "rectangle":
      return (
        <rect
          {...common}
          x={annotation.x}
          y={annotation.y}
          width={annotation.width}
          height={annotation.height}
          rx="4"
          fill={annotation.fill}
          stroke={annotation.color}
          strokeWidth={annotation.strokeWidth}
        />
      );
    case "ellipse":
      return (
        <ellipse
          {...common}
          cx={annotation.x + annotation.width / 2}
          cy={annotation.y + annotation.height / 2}
          rx={Math.abs(annotation.width / 2)}
          ry={Math.abs(annotation.height / 2)}
          fill={annotation.fill}
          stroke={annotation.color}
          strokeWidth={annotation.strokeWidth}
        />
      );
    case "arrow": {
      const angle = Math.atan2((annotation.endY ?? annotation.y) - annotation.y, (annotation.endX ?? annotation.x) - annotation.x);
      const tipX = annotation.endX ?? annotation.x;
      const tipY = annotation.endY ?? annotation.y;
      const size = Math.max(12, annotation.strokeWidth * 4);
      const wingA = `${tipX - size * Math.cos(angle - Math.PI / 6)},${tipY - size * Math.sin(angle - Math.PI / 6)}`;
      const wingB = `${tipX - size * Math.cos(angle + Math.PI / 6)},${tipY - size * Math.sin(angle + Math.PI / 6)}`;
      return (
        <g {...common} fill="none" stroke={annotation.color} strokeWidth={annotation.strokeWidth} strokeLinecap="round" strokeLinejoin="round">
          <line x1={annotation.x} y1={annotation.y} x2={tipX} y2={tipY} />
          <polyline points={`${wingA} ${tipX},${tipY} ${wingB}`} />
        </g>
      );
    }
    case "pen":
    case "highlighter":
      return (
        <polyline
          {...common}
          points={(annotation.points ?? []).map((point) => `${point.x},${point.y}`).join(" ")}
          fill="none"
          stroke={annotation.color}
          strokeWidth={annotation.strokeWidth}
          strokeLinecap="round"
          strokeLinejoin="round"
        />
      );
    case "text":
      return (
        <text
          {...common}
          x={annotation.x}
          y={annotation.y + annotation.fontSize}
          fill={annotation.color}
          fontSize={annotation.fontSize}
          fontWeight="600"
          fontFamily="Inter, 'Noto Sans CJK SC', sans-serif"
          style={{ userSelect: "none" }}
        >
          {textLines(annotation.text).map((line, index) => (
            <tspan key={index} x={annotation.x} dy={index === 0 ? 0 : annotation.fontSize * 1.3}>
              {line || "\u00a0"}
            </tspan>
          ))}
        </text>
      );
    case "area-highlight":
      return (
        <rect
          {...common}
          x={annotation.x}
          y={annotation.y}
          width={annotation.width}
          height={annotation.height}
          rx="3"
          fill="transparent"
          stroke="transparent"
          pointerEvents="all"
        />
      );
    case "mosaic":
      return pixelatedImage ? (
        <g {...common} opacity={1}>
          <image
            href={pixelatedImage}
            width={image.width}
            height={image.height}
            preserveAspectRatio="none"
            clipPath={`url(#clip-${annotation.id})`}
            style={{ imageRendering: "pixelated" }}
          />
          <rect x={annotation.x} y={annotation.y} width={annotation.width} height={annotation.height} fill="transparent" />
        </g>
      ) : (
        <rect
          {...common}
          opacity={1}
          x={annotation.x}
          y={annotation.y}
          width={annotation.width}
          height={annotation.height}
          fill="#202326"
        />
      );
  }
});

function Selection({
  annotation,
  zoom,
  onResizePointerDown,
}: {
  annotation: Annotation;
  zoom: number;
  onResizePointerDown: (event: ReactPointerEvent<SVGElement>, annotation: Annotation, handle: ResizeHandle) => void;
}) {
  if (annotation.type === "arrow") {
    const endX = annotation.endX ?? annotation.x;
    const endY = annotation.endY ?? annotation.y;
    return (
      <g className="editor-selection">
        <Handle x={annotation.x} y={annotation.y} zoom={zoom} cursor="move" onPointerDown={(event) => onResizePointerDown(event, annotation, "start")} />
        <Handle x={endX} y={endY} zoom={zoom} cursor="move" onPointerDown={(event) => onResizePointerDown(event, annotation, "end")} />
      </g>
    );
  }
  const bounds = annotationBounds(annotation);
  return (
    <g className="editor-selection">
      <rect className="selection-outline" x={bounds.x} y={bounds.y} width={bounds.width} height={bounds.height} vectorEffect="non-scaling-stroke" />
      <Handle x={bounds.x} y={bounds.y} zoom={zoom} cursor="nwse-resize" onPointerDown={(event) => onResizePointerDown(event, annotation, "nw")} />
      <Handle x={bounds.x + bounds.width} y={bounds.y} zoom={zoom} cursor="nesw-resize" onPointerDown={(event) => onResizePointerDown(event, annotation, "ne")} />
      <Handle x={bounds.x} y={bounds.y + bounds.height} zoom={zoom} cursor="nesw-resize" onPointerDown={(event) => onResizePointerDown(event, annotation, "sw")} />
      <Handle x={bounds.x + bounds.width} y={bounds.y + bounds.height} zoom={zoom} cursor="nwse-resize" onPointerDown={(event) => onResizePointerDown(event, annotation, "se")} />
    </g>
  );
}

function Handle({ x, y, zoom, cursor, onPointerDown }: { x: number; y: number; zoom: number; cursor: string; onPointerDown: (event: ReactPointerEvent<SVGCircleElement>) => void }) {
  return <circle className="selection-handle" cx={x} cy={y} r={7 / zoom} style={{ cursor }} vectorEffect="non-scaling-stroke" onPointerDown={onPointerDown} />;
}

function annotationBounds(annotation: Annotation) {
  if ((annotation.type === "pen" || annotation.type === "highlighter") && annotation.points?.length) {
    const xs = annotation.points.map((point) => point.x);
    const ys = annotation.points.map((point) => point.y);
    const x = Math.min(...xs);
    const y = Math.min(...ys);
    return { x, y, width: Math.max(MIN_SIZE, Math.max(...xs) - x), height: Math.max(MIN_SIZE, Math.max(...ys) - y) };
  }
  if (annotation.type === "text") {
    const lines = textLines(annotation.text);
    return {
      x: annotation.x,
      y: annotation.y,
      width: Math.max(annotation.width, Math.max(...lines.map((line) => Math.max(1, line.length))) * annotation.fontSize),
      height: Math.max(annotation.height, lines.length * annotation.fontSize * 1.3),
    };
  }
  return { x: annotation.x, y: annotation.y, width: Math.max(MIN_SIZE, annotation.width), height: Math.max(MIN_SIZE, annotation.height) };
}

function textLines(text: string | undefined) {
  return (text ?? "").split(/\r?\n/);
}

function moveAnnotation(annotation: Annotation, dx: number, dy: number): Annotation {
  if (annotation.type === "pen" || annotation.type === "highlighter") {
    return {
      ...annotation,
      x: annotation.x + dx,
      y: annotation.y + dy,
      points: annotation.points?.map((point) => ({ x: point.x + dx, y: point.y + dy })),
    };
  }
  return {
    ...annotation,
    x: annotation.x + dx,
    y: annotation.y + dy,
    endX: annotation.endX === undefined ? undefined : annotation.endX + dx,
    endY: annotation.endY === undefined ? undefined : annotation.endY + dy,
  };
}

function resizeAnnotation(annotation: Annotation, handle: ResizeHandle, dx: number, dy: number): Annotation {
  if (handle === "start") return { ...annotation, x: annotation.x + dx, y: annotation.y + dy };
  if (handle === "end") return { ...annotation, endX: (annotation.endX ?? annotation.x) + dx, endY: (annotation.endY ?? annotation.y) + dy };
  const originalBounds = annotationBounds(annotation);
  const resizedBounds = { ...originalBounds };
  if (handle.includes("n")) {
    resizedBounds.y = originalBounds.y + dy;
    resizedBounds.height = originalBounds.height - dy;
  }
  if (handle.includes("s")) resizedBounds.height = originalBounds.height + dy;
  if (handle.includes("w")) {
    resizedBounds.x = originalBounds.x + dx;
    resizedBounds.width = originalBounds.width - dx;
  }
  if (handle.includes("e")) resizedBounds.width = originalBounds.width + dx;
  const normalized = normalizeBox({ ...annotation, ...resizedBounds });
  if ((annotation.type === "pen" || annotation.type === "highlighter") && annotation.points?.length) {
    const scaleX = normalized.width / Math.max(MIN_SIZE, originalBounds.width);
    const scaleY = normalized.height / Math.max(MIN_SIZE, originalBounds.height);
    return {
      ...normalized,
      points: annotation.points.map((point) => ({
        x: normalized.x + (point.x - originalBounds.x) * scaleX,
        y: normalized.y + (point.y - originalBounds.y) * scaleY,
      })),
    };
  }
  if (annotation.type === "text") {
    return {
      ...normalized,
      fontSize: Math.max(12, annotation.fontSize * (normalized.height / Math.max(MIN_SIZE, originalBounds.height))),
    };
  }
  return normalized;
}

function loadImage(source: string): Promise<HTMLImageElement> {
  return new Promise((resolve, reject) => {
    const image = new Image();
    image.onload = () => resolve(image);
    image.onerror = () => reject(new Error("无法渲染导出图片"));
    image.src = source;
  });
}
