/**
 * Overlap detection and long-image assembly for scroll capture.
 *
 * Scroll capture grabs the same screen region repeatedly while the user
 * scrolls. Two consecutive frames share an overlapping strip (the bottom of
 * the old frame equals the top of the new one). We find that overlap by
 * comparing downsampled pixel rows/columns and then append only the newly
 * revealed pixels to the running long image.
 */

export type ScrollDirection = "vertical" | "horizontal";

/** Downsampled grayscale buffer for fast row/column comparison. */
interface Sample {
  width: number;
  height: number;
  data: Uint8Array;
  step: number;
}

function sampleImage(
  image: ImageData,
  step: number,
): Sample {
  const width = Math.ceil(image.width / step);
  const height = Math.ceil(image.height / step);
  const data = new Uint8Array(width * height);
  for (let y = 0; y < height; y++) {
    for (let x = 0; x < width; x++) {
      const sx = Math.min(image.width - 1, x * step);
      const sy = Math.min(image.height - 1, y * step);
      const offset = (sy * image.width + sx) * 4;
      // BT.601 luma; the alpha channel is ignored (screenshots are opaque).
      data[y * width + x] = (image.data[offset] * 299 + image.data[offset + 1] * 587 + image.data[offset + 2] * 114) >> 10;
    }
  }
  return { width, height, data, step };
}

/** Average per-pixel difference of a row band between two samples. */
function bandDifference(
  a: Sample,
  b: Sample,
  direction: ScrollDirection,
  aOffset: number,
  bOffset: number,
  length: number,
): number {
  let sum = 0;
  let count = 0;
  if (direction === "vertical") {
    if (aOffset < 0 || bOffset < 0 || aOffset + length > a.height || bOffset + length > b.height) {
      return Infinity;
    }
    for (let i = 0; i < length; i++) {
      const rowA = (aOffset + i) * a.width;
      const rowB = (bOffset + i) * b.width;
      for (let x = 0; x < a.width; x++) {
        sum += Math.abs(a.data[rowA + x] - b.data[rowB + x]);
        count++;
      }
    }
  } else {
    if (aOffset < 0 || bOffset < 0 || aOffset + length > a.width || bOffset + length > b.width) {
      return Infinity;
    }
    for (let i = 0; i < length; i++) {
      const colA = aOffset + i;
      const colB = bOffset + i;
      for (let y = 0; y < a.height; y++) {
        sum += Math.abs(a.data[y * a.width + colA] - b.data[y * a.width + colB]);
        count++;
      }
    }
  }
  return count === 0 ? Infinity : sum / count;
}

export interface ScrollOffset {
  /** Pixels the content moved between the two frames, along the scroll axis. */
  delta: number;
  /** 0..1 confidence of the match; below `confidence` the caller should wait. */
  confidence: number;
}

/**
 * Find how far the content scrolled between two consecutive captures of the
 * same region. `prev` is the older frame, `next` the newer one. For vertical
 * scrolling the content moves upward, so the overlap is `prev` bottom rows
 * against `next` top rows; the returned `delta` is positive and equals the
 * number of new pixels revealed at the bottom (i.e. `next` starts `delta`
 * rows below `prev`).
 *
 * Returns null when no trustworthy overlap exists (e.g. the frames are
 * unrelated or the content jumped too far).
 */
export function findScrollOffset(
  prev: ImageData,
  next: ImageData,
  direction: ScrollDirection,
): ScrollOffset | null {
  if (prev.width !== next.width || prev.height !== next.height) return null;
  const axis = direction === "vertical" ? prev.height : prev.width;
  const step = Math.max(1, Math.ceil(axis / 256));
  const a = sampleImage(prev, step);
  const b = sampleImage(next, step);

  const probeLength = Math.max(8, Math.floor(axis / step / 4));
  // The overlap strip must be at least this long (in sampled pixels) for the
  // match to be meaningful. Cap the search at half the frame so we never
  // accept a match that implies more than half the frame is new content.
  const maxDelta = Math.floor(axis / step / 2);
  const minProbe = Math.max(4, Math.floor(maxDelta / 2));

  let best: { delta: number; cost: number } | null = null;
  const costs: number[] = [];
  for (let delta = 1; delta <= maxDelta; delta++) {
    // Content scrolled along the axis by `delta`: the new frame's top strip is
    // exactly the old frame's strip that started at row/column `delta`. So
    // compare prev[delta..delta+avail] against next[0..avail].
    const available = Math.min(axis / step - delta, probeLength);
    if (available < minProbe) continue;
    const aStart = delta;
    const bStart = 0;
    const cost = bandDifference(a, b, direction, aStart, bStart, available);
    costs.push(cost);
    if (best === null || cost < best.cost) {
      best = { delta, cost };
    }
  }

  if (!best || costs.length < 3) return null;
  // Accept only when the winning overlap is clearly cheaper than the typical
  // band cost. A real scroll produces a sharp dip at the true offset; noise
  // yields several similar costs with no standout winner.
  const sorted = [...costs].sort((x, y) => x - y);
  const median = sorted[Math.floor(sorted.length / 2)];
  if (median <= 0 || best.cost >= 0.5 * median) return null;
  return { delta: best.delta * step, confidence: 1 - best.cost / median };
}

/**
 * Append a new frame to a running long canvas, skipping the `delta` pixels of
 * overlap with the previous frame. The first call (empty canvas) seeds the
 * long image with the whole frame. Returns the new canvas.
 */
export function appendFrame(
  canvas: HTMLCanvasElement,
  frame: ImageData,
  direction: ScrollDirection,
  delta: number,
  createCanvas: () => HTMLCanvasElement = () => document.createElement("canvas"),
): HTMLCanvasElement {
  const isVertical = direction === "vertical";
  const src = frameCanvas(frame, createCanvas);

  // First frame seeds the canvas.
  if (canvas.width === 0 || canvas.height === 0) {
    return src;
  }

  const oldWidth = canvas.width;
  const oldHeight = canvas.height;
  const added = Math.max(0, isVertical ? frame.height - delta : frame.width - delta);
  const nextWidth = isVertical ? Math.max(oldWidth, frame.width) : oldWidth + added;
  const nextHeight = isVertical ? oldHeight + added : Math.max(oldHeight, frame.height);

  const out = createCanvas();
  out.width = nextWidth;
  out.height = nextHeight;
  const outContext = out.getContext("2d");
  if (!outContext) return canvas;
  outContext.drawImage(canvas, 0, 0);
  if (isVertical) {
    // Content scrolled up: the frame's top `delta` rows overlap the previous
    // bottom; append the newly revealed bottom rows.
    outContext.drawImage(src, 0, delta, frame.width, added, 0, oldHeight, frame.width, added);
  } else {
    outContext.drawImage(src, delta, 0, added, frame.height, oldWidth, 0, added, frame.height);
  }
  return out;
}

function frameCanvas(frame: ImageData, createCanvas: () => HTMLCanvasElement): HTMLCanvasElement {
  const canvas = createCanvas();
  canvas.width = frame.width;
  canvas.height = frame.height;
  const context = canvas.getContext("2d");
  context?.putImageData(frame, 0, 0);
  return canvas;
}
