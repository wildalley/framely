import { beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  invoke: vi.fn(),
  save: vi.fn(),
}));

vi.mock("@tauri-apps/api/core", () => ({ invoke: mocks.invoke }));
vi.mock("@tauri-apps/plugin-dialog", () => ({ open: vi.fn(), save: mocks.save }));

import { saveImageAs, saveProject } from "./platform";

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
