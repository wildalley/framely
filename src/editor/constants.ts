import {
  Circle,
  Grid3X3,
  Highlighter,
  MousePointer2,
  MoveUpRight,
  Pencil,
  Square,
  SquareDashed,
  Type,
} from "lucide-react";
import type { LucideIcon } from "lucide-react";
import type { Annotation, ImageSource, Tool } from "../types";

export type EditorPanel = "none" | "style" | "menu" | "shortcuts" | "settings";

export interface ToolItem {
  id: Tool;
  label: string;
  icon: LucideIcon;
  key: string;
}

export const TOOL_ITEMS: ToolItem[] = [
  { id: "select", label: "选择", icon: MousePointer2, key: "V" },
  { id: "rectangle", label: "矩形", icon: Square, key: "R" },
  { id: "ellipse", label: "椭圆", icon: Circle, key: "O" },
  { id: "arrow", label: "箭头", icon: MoveUpRight, key: "A" },
  { id: "pen", label: "画笔", icon: Pencil, key: "P" },
  { id: "highlighter", label: "荧光笔", icon: Highlighter, key: "H" },
  { id: "area-highlight", label: "区域高亮", icon: SquareDashed, key: "G" },
  { id: "text", label: "文字", icon: Type, key: "T" },
  { id: "mosaic", label: "马赛克", icon: Grid3X3, key: "M" },
];

export const DRAW_TOOLS = TOOL_ITEMS.map((item) => item.id).filter(
  (id): id is Annotation["type"] => id !== "select",
);

export const MIN_ZOOM = 0.1;
export const MAX_ZOOM = 3;
export const MIN_WINDOW_WIDTH = 320;
export const EMPTY_WINDOW = { width: 460, height: 300 };
export const ASSUMED_BAR_HEIGHT = 46;
/** Width each popover is designed for; the window grows to fit it while open. */
export const PANEL_WIDTH: Record<Exclude<EditorPanel, "none">, number> = {
  style: 248,
  menu: 248,
  shortcuts: 340,
  settings: 420,
};
export const CAPTURE_SETTLE_DELAY = 150;

export const clamp = (value: number, min: number, max: number) => Math.min(max, Math.max(min, value));

export const roundZoom = (value: number) => Math.round(value * 100) / 100;

/** Largest zoom that keeps image plus toolbar inside the usable desktop area. */
export function zoomToFit(image: ImageSource, area: { width: number; height: number } | null, barHeight: number) {
  if (!area) return 1;
  const usableWidth = Math.max(120, area.width - 24);
  const usableHeight = Math.max(120, area.height - barHeight - 24);
  return roundZoom(clamp(Math.min(usableWidth / image.width, usableHeight / image.height, 1), MIN_ZOOM, 1));
}

export function toolName(tool: Annotation["type"]) {
  return TOOL_ITEMS.find((item) => item.id === tool)?.label ?? tool;
}

/** Matches `CAPTURE_CANCELLED` in capture.rs. */
export const CAPTURE_CANCELLED = "截图已取消";

export function errorMessage(error: unknown) {
  return error instanceof Error ? error.message : String(error);
}
