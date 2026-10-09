export interface Point {
  x: number;
  y: number;
}

export interface Rect {
  x: number;
  y: number;
  width: number;
  height: number;
}

export const clamp = (value: number, min: number, max: number) => Math.min(max, Math.max(min, value));

/** The rectangle spanned by two corners, whichever way the drag went. */
export const boxOf = (a: Point, b: Point): Rect => ({
  x: Math.min(a.x, b.x),
  y: Math.min(a.y, b.y),
  width: Math.abs(b.x - a.x),
  height: Math.abs(b.y - a.y),
});

/**
 * Shift both corners of a drag by (dx, dy), stopping at the view's edges so
 * the box keeps its size instead of being squashed against them.
 */
export function shiftCorners(
  start: Point,
  end: Point,
  dx: number,
  dy: number,
  view: { width: number; height: number },
): [Point, Point] {
  const x = clamp(dx, -Math.min(start.x, end.x), view.width - Math.max(start.x, end.x));
  const y = clamp(dy, -Math.min(start.y, end.y), view.height - Math.max(start.y, end.y));
  return [
    { x: start.x + x, y: start.y + y },
    { x: end.x + x, y: end.y + y },
  ];
}
