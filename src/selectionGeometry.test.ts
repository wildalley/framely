import { describe, expect, it } from "vitest";
import { boxOf, shiftCorners } from "./selectionGeometry";

const view = { width: 1000, height: 600 };

describe("selection geometry", () => {
  it("spans the same box whichever way the drag goes", () => {
    expect(boxOf({ x: 300, y: 200 }, { x: 100, y: 50 })).toEqual({ x: 100, y: 50, width: 200, height: 150 });
  });

  it("moves a box without changing its size", () => {
    const [start, end] = shiftCorners({ x: 100, y: 100 }, { x: 300, y: 200 }, 50, -30, view);
    expect(boxOf(start, end)).toEqual({ x: 150, y: 70, width: 200, height: 100 });
  });

  it("stops at the edges instead of squashing the box", () => {
    const [start, end] = shiftCorners({ x: 900, y: 10 }, { x: 700, y: 110 }, 500, -500, view);
    expect(boxOf(start, end)).toEqual({ x: 800, y: 0, width: 200, height: 100 });
  });
});
