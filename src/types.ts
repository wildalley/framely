export type Tool =
  | "select"
  | "rectangle"
  | "ellipse"
  | "arrow"
  | "pen"
  | "highlighter"
  | "area-highlight"
  | "text"
  | "mosaic";

export interface Point {
  x: number;
  y: number;
}

export interface Annotation {
  id: string;
  type: Exclude<Tool, "select">;
  x: number;
  y: number;
  width: number;
  height: number;
  endX?: number;
  endY?: number;
  points?: Point[];
  text?: string;
  color: string;
  fill: string;
  strokeWidth: number;
  opacity: number;
  fontSize: number;
}

export interface ImageSource {
  dataUrl: string;
  width: number;
  height: number;
  name: string;
}

export interface ScreenPinProject {
  format: "screenpin-project";
  version: 1;
  title: string;
  createdAt: string;
  updatedAt: string;
  image: ImageSource;
  annotations: Annotation[];
}

export interface SavedDocument {
  imageDataUrl: string;
  title: string;
  createdAt: string;
  annotations: string;
}

export interface CaptureResult {
  dataUrl: string;
  width: number;
  height: number;
}

/** A monitor's location inside a whole-desktop capture, in image pixels. */
export interface ScreenBounds {
  x: number;
  y: number;
  width: number;
  height: number;
  /** True for the system's primary monitor. */
  primary: boolean;
}

export interface CaptureResponse extends CaptureResult {
  selectionRequired: boolean;
  fallbackUsed: boolean;
  fallbackReason?: string | null;
  /** The monitors inside the delivered bitmap, empty when unmappable. */
  screens: ScreenBounds[];
}

export const COLORS = ["#e5484d", "#ff8a3d", "#f2c94c", "#1f9d73", "#2383e2", "#7c5cff", "#171717", "#ffffff"];

/** Formats the editor can export to. PNG stays lossless, the others take a quality. */
export type ExportFormat = "png" | "jpeg" | "webp";

export const EXPORT_FORMATS: { id: ExportFormat; label: string; extension: string; lossy: boolean }[] = [
  { id: "png", label: "PNG", extension: "png", lossy: false },
  { id: "jpeg", label: "JPEG", extension: "jpg", lossy: true },
  { id: "webp", label: "WebP", extension: "webp", lossy: true },
];

export const mimeForFormat = (format: ExportFormat) => `image/${format}` as const;

export const extensionForFormat = (format: ExportFormat) =>
  EXPORT_FORMATS.find((entry) => entry.id === format)?.extension ?? format;

export const isLossyFormat = (format: ExportFormat) =>
  EXPORT_FORMATS.find((entry) => entry.id === format)?.lossy ?? false;

/** The editable look of an annotation, kept per tool so each tool remembers its own setup. */
export interface AnnotationStyle {
  color: string;
  strokeWidth: number;
  opacity: number;
  fontSize: number;
}

export const defaultStyle = (type: Annotation["type"]): AnnotationStyle => ({
  color: type === "highlighter" || type === "area-highlight" ? "#f2c94c" : "#e5484d",
  strokeWidth: type === "highlighter" ? 18 : type === "area-highlight" ? 2 : 4,
  opacity: type === "highlighter" ? 0.45 : type === "area-highlight" ? 0.55 : 1,
  fontSize: 28,
});

export const createAnnotation = (
  type: Annotation["type"],
  point: Point,
  style: AnnotationStyle = defaultStyle(type),
): Annotation => ({
  id: crypto.randomUUID(),
  type,
  x: point.x,
  y: point.y,
  width: 0,
  height: 0,
  endX: point.x,
  endY: point.y,
  points: type === "pen" || type === "highlighter" ? [point] : undefined,
  text: type === "text" ? "输入文字" : undefined,
  fill: "transparent",
  ...style,
});

export const normalizeBox = (annotation: Annotation): Annotation => {
  if (annotation.width >= 0 && annotation.height >= 0) return annotation;
  return {
    ...annotation,
    x: annotation.width < 0 ? annotation.x + annotation.width : annotation.x,
    y: annotation.height < 0 ? annotation.y + annotation.height : annotation.y,
    width: Math.abs(annotation.width),
    height: Math.abs(annotation.height),
  };
};

export function isDocumentDirty(
  image: ImageSource | null,
  title: string,
  createdAt: string,
  annotations: Annotation[],
  saved: SavedDocument | null,
) {
  if (!image) return false;
  if (!saved) return true;
  return (
    saved.imageDataUrl !== image.dataUrl ||
    saved.title !== title ||
    saved.createdAt !== createdAt ||
    saved.annotations !== JSON.stringify(annotations)
  );
}

export const isScreenPinProject = (value: unknown): value is ScreenPinProject => {
  if (!value || typeof value !== "object") return false;
  const project = value as Partial<ScreenPinProject>;
  if (!(
    project.format === "screenpin-project" &&
    project.version === 1 &&
    typeof project.title === "string" &&
    project.title.length <= 500 &&
    isIsoDate(project.createdAt) &&
    isIsoDate(project.updatedAt) &&
    isImageSource(project.image) &&
    Array.isArray(project.annotations)
  )) return false;

  if (project.annotations.length > 10_000) return false;
  const ids = new Set<string>();
  let pointCount = 0;
  for (const annotation of project.annotations) {
    if (!isAnnotation(annotation) || ids.has(annotation.id)) return false;
    ids.add(annotation.id);
    pointCount += annotation.points?.length ?? 0;
    if (pointCount > 500_000) return false;
  }
  return true;
};

const ANNOTATION_TYPES = new Set<Annotation["type"]>([
  "rectangle",
  "ellipse",
  "arrow",
  "pen",
  "highlighter",
  "area-highlight",
  "text",
  "mosaic",
]);

const isFiniteNumber = (value: unknown): value is number => typeof value === "number" && Number.isFinite(value);
const isCoordinate = (value: unknown) => isFiniteNumber(value) && Math.abs(value) <= 1_000_000;
const isHexColor = (value: unknown) => typeof value === "string" && /^#[0-9a-f]{6}$/i.test(value);
const isIsoDate = (value: unknown) =>
  typeof value === "string" && value.length <= 64 && Number.isFinite(Date.parse(value));

function isImageSource(value: unknown): value is ImageSource {
  if (!value || typeof value !== "object") return false;
  const image = value as Partial<ImageSource>;
  return (
    typeof image.dataUrl === "string" &&
    image.dataUrl.length <= 150_000_000 &&
    /^data:image\/(?:png|jpeg|webp);base64,/i.test(image.dataUrl) &&
    isFiniteNumber(image.width) && image.width > 0 && image.width <= 32_768 &&
    isFiniteNumber(image.height) && image.height > 0 && image.height <= 32_768 &&
    image.width * image.height <= 100_000_000 &&
    typeof image.name === "string" && image.name.length <= 500
  );
}

function isAnnotation(value: unknown): value is Annotation {
  if (!value || typeof value !== "object") return false;
  const annotation = value as Partial<Annotation>;
  if (
    typeof annotation.id !== "string" || annotation.id.length === 0 || annotation.id.length > 200 ||
    !ANNOTATION_TYPES.has(annotation.type as Annotation["type"]) ||
    !isCoordinate(annotation.x) || !isCoordinate(annotation.y) ||
    !isCoordinate(annotation.width) || !isCoordinate(annotation.height) ||
    !isHexColor(annotation.color) ||
    !(annotation.fill === "transparent" || isHexColor(annotation.fill)) ||
    !isFiniteNumber(annotation.strokeWidth) || annotation.strokeWidth < 0 || annotation.strokeWidth > 512 ||
    !isFiniteNumber(annotation.opacity) || annotation.opacity < 0 || annotation.opacity > 1 ||
    !isFiniteNumber(annotation.fontSize) || annotation.fontSize < 1 || annotation.fontSize > 1_000
  ) return false;

  if (annotation.endX !== undefined && !isCoordinate(annotation.endX)) return false;
  if (annotation.endY !== undefined && !isCoordinate(annotation.endY)) return false;
  if (annotation.type === "arrow" && (!isCoordinate(annotation.endX) || !isCoordinate(annotation.endY))) return false;
  if (annotation.type === "text" && (typeof annotation.text !== "string" || annotation.text.length > 100_000)) return false;
  if (annotation.type !== "text" && annotation.text !== undefined) return false;
  if (annotation.type === "mosaic" && annotation.opacity !== 1) return false;
  if (annotation.type === "pen" || annotation.type === "highlighter") {
    if (!Array.isArray(annotation.points) || annotation.points.length === 0 || annotation.points.length > 50_000) return false;
    if (!annotation.points.every((point) => point && isCoordinate(point.x) && isCoordinate(point.y))) return false;
  } else if (annotation.points !== undefined) {
    return false;
  }
  return true;
}
