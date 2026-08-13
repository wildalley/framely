import { describe, expect, it, vi } from "vitest";
import { cropCapture, moveSelection, normalizeSelection, resizeSelection } from "./CaptureOverlay";

describe("capture selection geometry", () => {
  it("normalizes a drag in any direction", () => {
    expect(normalizeSelection({ x: 90, y: 70 }, { x: 20, y: 10 })).toEqual({
      x: 20,
      y: 10,
      width: 70,
      height: 60,
    });
  });

  it("keeps a moved selection inside the captured monitor", () => {
    const selection = { x: 40, y: 30, width: 100, height: 80 };
    expect(moveSelection(selection, { x: -100, y: 200 }, { width: 300, height: 180 })).toEqual({
      x: 0,
      y: 100,
      width: 100,
      height: 80,
    });
  });

  it("resizes from corners without crossing the opposite edge", () => {
    const selection = { x: 40, y: 30, width: 100, height: 80 };
    expect(resizeSelection(selection, "nw", { x: 10, y: 5 }, { width: 300, height: 180 })).toEqual({
      x: 10,
      y: 5,
      width: 130,
      height: 105,
    });
    expect(resizeSelection(selection, "se", { x: 500, y: 500 }, { width: 300, height: 180 })).toEqual({
      x: 40,
      y: 30,
      width: 260,
      height: 150,
    });
  });
});

describe("capture crop", () => {
  it("draws the selected source pixels into a same-sized PNG canvas", async () => {
    const drawImage = vi.fn();
    const canvas = {
      width: 0,
      height: 0,
      getContext: vi.fn(() => ({ drawImage })),
      toDataURL: vi.fn(() => "data:image/png;base64,cropped"),
    };
    class FakeImage {
      naturalWidth = 800;
      naturalHeight = 600;
      onload: (() => void) | null = null;

      set src(_value: string) {
        queueMicrotask(() => this.onload?.());
      }
    }
    vi.stubGlobal("Image", FakeImage);
    vi.stubGlobal("document", { createElement: vi.fn(() => canvas) });

    await expect(cropCapture(
      { dataUrl: "data:image/png;base64,source", width: 800, height: 600 },
      { x: 120, y: 80, width: 320, height: 240 },
    )).resolves.toEqual({
      dataUrl: "data:image/png;base64,cropped",
      width: 320,
      height: 240,
    });
    expect(canvas).toMatchObject({ width: 320, height: 240 });
    expect(drawImage).toHaveBeenCalledWith(expect.any(FakeImage), 120, 80, 320, 240, 0, 0, 320, 240);
  });
});
