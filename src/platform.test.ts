import { beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  confirm: vi.fn(),
  invoke: vi.fn(),
  save: vi.fn(),
}));

vi.mock("@tauri-apps/api/core", () => ({ invoke: mocks.invoke }));
vi.mock("@tauri-apps/plugin-dialog", () => ({ confirm: mocks.confirm, open: vi.fn(), save: mocks.save }));

import { confirmDiscardChanges, saveImageAs, saveProject, stitchImages, validateProjectImage } from "./platform";

const project = {
  format: "screenpin-project" as const,
  version: 1 as const,
  title: "cancelled",
  createdAt: "2026-08-11T00:00:00.000Z",
  updatedAt: "2026-08-11T00:00:00.000Z",
  image: { dataUrl: "data:image/png;base64,AA==", width: 1, height: 1, name: "pixel" },
  annotations: [],
};

describe("desktop save cancellation", () => {
  beforeEach(() => {
    vi.stubGlobal("window", { __TAURI_INTERNALS__: {} });
    mocks.invoke.mockReset();
    mocks.confirm.mockReset();
    mocks.save.mockReset().mockResolvedValue(null);
  });

  it("does not report or write a cancelled image save", async () => {
    await expect(saveImageAs(project.image.dataUrl, "image", "png")).resolves.toBe(false);
    expect(mocks.invoke).not.toHaveBeenCalled();
  });

  it("does not report or write a cancelled project save", async () => {
    await expect(saveProject(project)).resolves.toBe(false);
    expect(mocks.invoke).not.toHaveBeenCalled();
  });
});

describe("destructive action confirmation", () => {
  beforeEach(() => {
    vi.stubGlobal("window", { __TAURI_INTERNALS__: {} });
    mocks.confirm.mockReset().mockResolvedValue(false);
  });

  it("uses the native warning dialog and preserves cancellation", async () => {
    await expect(confirmDiscardChanges()).resolves.toBe(false);
    expect(mocks.confirm).toHaveBeenCalledWith(
      expect.stringContaining("未保存"),
      expect.objectContaining({ kind: "warning" }),
    );
  });
});

describe("project image validation", () => {
  class FakeImage {
    naturalWidth = 10;
    naturalHeight = 20;
    onload: (() => void) | null = null;
    onerror: (() => void) | null = null;

    set src(value: string) {
      queueMicrotask(() => (value.includes("broken") ? this.onerror?.() : this.onload?.()));
    }
  }

  beforeEach(() => vi.stubGlobal("Image", FakeImage));

  it("accepts a decodable image with matching dimensions", async () => {
    await expect(
      validateProjectImage({ dataUrl: "data:image/png;base64,AA==", width: 10, height: 20, name: "ok" }),
    ).resolves.toBeUndefined();
  });

  it("rejects corrupt data and declared dimension mismatches", async () => {
    await expect(
      validateProjectImage({ dataUrl: "data:image/png;base64,broken", width: 10, height: 20, name: "bad" }),
    ).rejects.toThrow("已损坏");
    await expect(
      validateProjectImage({ dataUrl: "data:image/png;base64,AA==", width: 11, height: 20, name: "bad-size" }),
    ).rejects.toThrow("尺寸");
  });
});

describe("image stitching", () => {
  class FakeImage {
    naturalWidth = 0;
    naturalHeight = 0;
    width = 0;
    height = 0;
    onload: (() => void) | null = null;
    onerror: (() => void) | null = null;

    set src(value: string) {
      const dims = value.split("dims=")[1];
      const [w, h] = (dims ?? "1,1").split(",").map(Number);
      this.naturalWidth = w;
      this.naturalHeight = h;
      this.width = w;
      this.height = h;
      queueMicrotask(() => this.onload?.());
    }
  }

  const makeCanvas = () => {
    const drawImage = vi.fn();
    const canvas = {
      width: 0,
      height: 0,
      getContext: vi.fn(() => ({ drawImage })),
      toDataURL: vi.fn(() => "data:image/png;base64,stitched"),
    };
    return { canvas, drawImage };
  };

  beforeEach(() => vi.stubGlobal("Image", FakeImage));

  it("stacks images vertically and centers them on the width", async () => {
    const { canvas, drawImage } = makeCanvas();
    vi.stubGlobal("document", { createElement: vi.fn(() => canvas) });

    const stitched = await stitchImages(
      [
        { dataUrl: "data:image/png;base64,x;dims=100,50", width: 100, height: 50 },
        { dataUrl: "data:image/png;base64,y;dims=80,60", width: 80, height: 60 },
      ],
      "vertical",
    );

    expect(stitched).toEqual({ dataUrl: "data:image/png;base64,stitched", width: 100, height: 110 });
    expect(canvas).toMatchObject({ width: 100, height: 110 });
    // Second image (80 wide) is centered in the 100-wide canvas.
    expect(drawImage).toHaveBeenNthCalledWith(2, expect.any(FakeImage), 10, 50);
  });

  it("lays images side by side for horizontal stitching", async () => {
    const { canvas, drawImage } = makeCanvas();
    vi.stubGlobal("document", { createElement: vi.fn(() => canvas) });

    await stitchImages(
      [
        { dataUrl: "data:image/png;base64,x;dims=100,50", width: 100, height: 50 },
        { dataUrl: "data:image/png;base64,y;dims=80,60", width: 80, height: 60 },
      ],
      "horizontal",
    );

    expect(canvas).toMatchObject({ width: 180, height: 60 });
    // Second image is placed right after the first and centered vertically.
    expect(drawImage).toHaveBeenNthCalledWith(2, expect.any(FakeImage), 100, 0);
  });
});
