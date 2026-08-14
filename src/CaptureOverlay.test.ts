import { describe, expect, it, vi } from "vitest";
import {
  cropCapture,
  cropThumb,
  imageDisplayRect,
  moveSelection,
  normalizeSelection,
  resizeSelection,
  screenAtPoint,
  screenLayoutAxis,
  unconstainPoint,
} from "./CaptureOverlay";

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

  it("crops a single screen's thumbnail without nearby screens", async () => {
    const drawImage = vi.fn();
    const context = { drawImage, imageSmoothingQuality: "low" };
    const canvas = {
      width: 0,
      height: 0,
      getContext: vi.fn(() => context),
      toDataURL: vi.fn(() => "data:image/png;base64,thumb"),
    };
    class FakeImage {
      naturalWidth = 5120;
      naturalHeight = 1622;
      onload: (() => void) | null = null;

      set src(_value: string) {
        queueMicrotask(() => this.onload?.());
      }
    }
    vi.stubGlobal("Image", FakeImage);
    vi.stubGlobal("document", { createElement: vi.fn(() => canvas) });

    const screen = { x: 2560, y: 0, width: 2560, height: 1440, primary: false, originX: 2560, originY: 0, cursorHere: false };
    await expect(cropThumb(
      { dataUrl: "data:image/png;base64,source", width: 5120, height: 1622 },
      screen,
      1350,
      760,
    )).resolves.toBe("data:image/png;base64,thumb");
    // The right monitor's region is copied at the requested sharp size.
    expect(drawImage).toHaveBeenCalledWith(expect.any(FakeImage), 2560, 0, 2560, 1440, 0, 0, 1350, 760);
  });
});

describe("virtual-desktop display mapping", () => {
  const image = { width: 5120, height: 1622 };

  it("fits a wide virtual desktop inside a single-screen selector window", () => {
    const surface = { left: 0, top: 0, width: 1920, height: 1080 };
    const rect = imageDisplayRect(surface, image);
    // Same aspect ratio as the desktop bitmap: fills the whole window.
    expect(rect.height).toBeCloseTo(image.height * (1920 / image.width), 0.001);
    expect(rect.top).toBeCloseTo((1080 - rect.height) / 2, 0.001);
  });

  it("maps a click on the letterboxed image back to desktop pixels", () => {
    const surface = { left: 0, top: 0, width: 1920, height: 1080 };
    const rect = imageDisplayRect(surface, image);
    // Clicking the very top-left of the image area is desktop (0, 0).
    expect(unconstainPoint(rect, image, rect.left, rect.top)).toEqual({ x: 0, y: 0 });
    // Clicking the bottom-right of the image area is the far desktop corner.
    const bottomRight = unconstainPoint(rect, image, rect.left + rect.width, rect.top + rect.height);
    expect(bottomRight.x).toBeCloseTo(image.width);
    expect(bottomRight.y).toBeCloseTo(image.height);
  });

  it("guards against a degenerate or missing surface", () => {
    expect(imageDisplayRect({ left: 0, top: 0, width: 0, height: 0 }, image))
      .toEqual({ left: 0, top: 0, width: 0, height: 0 });
  });
});

describe("screen picking", () => {
  const screens = [
    { x: 0, y: 0, width: 1920, height: 1080, primary: true, originX: 0, originY: 0, cursorHere: false },
    { x: 1920, y: 0, width: 1920, height: 1080, primary: false, originX: 1920, originY: 0, cursorHere: false },
  ];

  it("finds the monitor that contains an image-pixel point", () => {
    expect(screenAtPoint(screens, { x: 100, y: 100 })).toEqual(screens[0]);
    expect(screenAtPoint(screens, { x: 2000, y: 500 })).toEqual(screens[1]);
  });

  it("rejects points outside every monitor", () => {
    expect(screenAtPoint(screens, { x: -10, y: 100 })).toBeUndefined();
    expect(screenAtPoint(screens, { x: 5000, y: 100 })).toBeUndefined();
  });

  it("lays side-by-side monitors in a row", () => {
    expect(screenLayoutAxis(screens)).toBe("row");
  });

  it("lays stacked monitors in a column", () => {
    expect(
      screenLayoutAxis([
        { x: 0, y: 0, width: 1920, height: 1080, primary: true, originX: 0, originY: 0, cursorHere: false },
        { x: 0, y: 1080, width: 1920, height: 1080, primary: false, originX: 0, originY: 1080, cursorHere: false },
      ]),
    ).toBe("column");
  });
});
