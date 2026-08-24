import { describe, expect, it } from "vitest";
import { appendFrame, findScrollOffset } from "./stitch";

/** Minimal canvas double for the node test environment. */
function makeCanvas() {
  const state: { pixels: Map<string, [number, number, number, number]>; width: number; height: number } = {
    pixels: new Map(),
    width: 0,
    height: 0,
  };
  const canvas = {
    width: 0,
    height: 0,
    getContext: () => ({
      putImageData: (image: ImageData, dx: number, dy: number) => {
        state.width = image.width;
        state.height = image.height;
        for (let y = 0; y < image.height; y++) {
          for (let x = 0; x < image.width; x++) {
            const o = (y * image.width + x) * 4;
            state.pixels.set(`${x + dx},${y + dy}`, [
              image.data[o],
              image.data[o + 1],
              image.data[o + 2],
              image.data[o + 3],
            ]);
          }
        }
      },
      drawImage: () => {
        // no-op; size checks are what the tests assert.
      },
    }),
  };
  return canvas;
}

/**
 * Simulate scrolling: build one tall "page" of unique rows, then take two
 * windows of it as consecutive capture frames. `next` starts `scroll` rows
 * further down, so its top overlaps `prev`'s bottom by height - scroll.
 */
function pageImage(width: number, pageHeight: number): Uint8ClampedArray {
  const data = new Uint8ClampedArray(width * pageHeight * 4);
  let seed = 0x9e3779b9;
  for (let y = 0; y < pageHeight; y++) {
    seed = (seed * 1664525 + 1013904223) >>> 0;
    const rowSeed = seed;
    for (let x = 0; x < width; x++) {
      const offset = (y * width + x) * 4;
      const value = ((rowSeed >> ((x % 4) * 8)) ^ (x * 37)) & 0xff;
      data[offset] = value;
      data[offset + 1] = value ^ 0x55;
      data[offset + 2] = (value + 77) & 0xff;
      data[offset + 3] = 255;
    }
  }
  return data;
}

function windowImage(page: Uint8ClampedArray, width: number, height: number, top: number): ImageData {
  const data = page.slice(top * width * 4, (top + height) * width * 4);
  return { width, height, data } as ImageData;
}

describe("findScrollOffset", () => {
  it("detects a vertical scroll by the number of shifted rows", () => {
    const width = 80;
    const height = 200;
    const scroll = 30;
    const page = pageImage(width, height + scroll);
    const prev = windowImage(page, width, height, 0);
    const next = windowImage(page, width, height, scroll);
    const result = findScrollOffset(prev, next, "vertical");
    expect(result).not.toBeNull();
    // The overlap is height - scroll = 170 rows; the detector reports the
    // pixels of new content revealed (scroll amount), within step quantization.
    expect(result!.delta).toBeGreaterThanOrEqual(20);
    expect(result!.delta).toBeLessThanOrEqual(40);
  });

  it("ignores a fixed header while matching scrolled page content", () => {
    const width = 80;
    const height = 200;
    const scroll = 28;
    const header = 24;
    const page = pageImage(width, height + scroll);
    const prev = windowImage(page, width, height, 0);
    const next = windowImage(page, width, height, scroll);
    // Simulate a sticky header that stays unchanged at the top of both frames.
    next.data.set(prev.data.subarray(0, header * width * 4), 0);
    const result = findScrollOffset(prev, next, "vertical");
    expect(result).not.toBeNull();
    expect(result!.delta).toBeGreaterThanOrEqual(18);
    expect(result!.delta).toBeLessThanOrEqual(38);
  });

  it("returns null when frames are unrelated", () => {
    const a = pageImage(60, 120);
    const b = pageImage(60, 120);
    for (let offset = 0; offset < b.length; offset += 4) {
      b[offset] ^= 0xff;
      b[offset + 1] = (b[offset + 1] + 91) & 0xff;
    }
    const prev = windowImage(a, 60, 120, 0);
    const next = windowImage(b, 60, 120, 0);
    const result = findScrollOffset(prev, next, "vertical");
    if (result) {
      // A false positive must not look like a confident small scroll.
      expect(result.confidence).toBeLessThan(0.5);
    }
  });

  it("returns null when the page has not moved", () => {
    const page = pageImage(60, 120);
    const frame = windowImage(page, 60, 120, 0);
    expect(findScrollOffset(frame, frame, "vertical")).toBeNull();
  });

  it("detects horizontal scrolling", () => {
    const height = 80;
    const width = 200;
    const scroll = 36;
    const page = pageImage(height, width + scroll);
    const transpose = (left: number): ImageData => {
      const data = new Uint8ClampedArray(width * height * 4);
      for (let y = 0; y < height; y++) {
        for (let x = 0; x < width; x++) {
          const source = ((left + x) * height + y) * 4;
          data.set(page.subarray(source, source + 4), (y * width + x) * 4);
        }
      }
      return { width, height, data } as ImageData;
    };
    const result = findScrollOffset(transpose(0), transpose(scroll), "horizontal");
    expect(result).not.toBeNull();
    expect(result!.delta).toBeGreaterThanOrEqual(25);
    expect(result!.delta).toBeLessThanOrEqual(45);
  });

  it("rejects frames of different sizes", () => {
    const a = pageImage(60, 120);
    const prev = windowImage(a, 60, 120, 0);
    const next = windowImage(a, 70, 120, 10);
    expect(findScrollOffset(prev, next, "vertical")).toBeNull();
  });
});

describe("appendFrame", () => {
  it("seeds the long canvas with the first frame", () => {
    const canvas = makeCanvas() as unknown as HTMLCanvasElement;
    const page = pageImage(40, 60);
    const frame = windowImage(page, 40, 50, 0);
    const out = appendFrame(canvas, frame, "vertical", 0, () => makeCanvas() as unknown as HTMLCanvasElement);
    expect(out.width).toBe(40);
    expect(out.height).toBe(50);
  });

  it("appends only the newly revealed rows after a scroll", () => {
    const canvas = makeCanvas() as unknown as HTMLCanvasElement;
    const page = pageImage(40, 120);
    const first = windowImage(page, 40, 50, 0);
    const seeded = appendFrame(canvas, first, "vertical", 0, () => makeCanvas() as unknown as HTMLCanvasElement);
    // Second frame scrolls down by 10 rows, revealing 10 new rows.
    const second = windowImage(page, 40, 50, 10);
    const out = appendFrame(seeded, second, "vertical", 10, () => makeCanvas() as unknown as HTMLCanvasElement);
    expect(out.height).toBe(50 + 10);
    expect(out.width).toBe(40);
  });

  it("appends only the newly revealed columns after horizontal scrolling", () => {
    const canvas = makeCanvas() as unknown as HTMLCanvasElement;
    const first = windowImage(pageImage(50, 40), 50, 40, 0);
    const seeded = appendFrame(canvas, first, "horizontal", 0, () => makeCanvas() as unknown as HTMLCanvasElement);
    const out = appendFrame(seeded, first, "horizontal", 12, () => makeCanvas() as unknown as HTMLCanvasElement);
    expect(out.width).toBe(62);
    expect(out.height).toBe(40);
  });
});
