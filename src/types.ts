export type Tool =
  | "select"
  | "rectangle"
  | "ellipse"
  | "arrow"
  | "pen"
  | "highlighter"
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

export interface CaptureResult {
  dataUrl: string;
  width: number;
  height: number;
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
  color: type === "highlighter" ? "#f2c94c" : "#e5484d",
  strokeWidth: type === "highlighter" ? 18 : 4,
  opacity: type === "highlighter" ? 0.45 : 1,
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

export const isScreenPinProject = (value: unknown): value is ScreenPinProject => {
  if (!value || typeof value !== "object") return false;
  const project = value as Partial<ScreenPinProject>;
  return (
    project.format === "screenpin-project" &&
    project.version === 1 &&
    typeof project.title === "string" &&
    typeof project.image?.dataUrl === "string" &&
    typeof project.image?.width === "number" &&
    typeof project.image?.height === "number" &&
    Array.isArray(project.annotations)
  );
};
