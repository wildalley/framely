import { beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  confirm: vi.fn(),
  invoke: vi.fn(),
  save: vi.fn(),
}));

vi.mock("@tauri-apps/api/core", () => ({ invoke: mocks.invoke }));
vi.mock("@tauri-apps/plugin-dialog", () => ({ confirm: mocks.confirm, open: vi.fn(), save: mocks.save }));

import { confirmDiscardChanges, saveImageAs, saveProject, validateProjectImage } from "./platform";

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
