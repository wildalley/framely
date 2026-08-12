import { describe, expect, it } from "vitest";
import {
  extensionForFormat,
  isLossyFormat,
  isScreenPinProject,
  mimeForFormat,
  normalizeBox,
  type Annotation,
  type ScreenPinProject,
} from "./types";

const annotation: Annotation = {
  id: "shape-1",
  type: "rectangle",
  x: 100,
  y: 80,
  width: -40,
  height: -30,
  color: "#e5484d",
  fill: "transparent",
  strokeWidth: 4,
  opacity: 1,
  fontSize: 28,
};

describe("normalizeBox", () => {
  it("preserves the visual bounds when drawing backwards", () => {
    expect(normalizeBox(annotation)).toMatchObject({ x: 60, y: 50, width: 40, height: 30 });
  });
});

describe("isScreenPinProject", () => {
  it("recognizes a versioned editable project", () => {
    const project: ScreenPinProject = {
      format: "screenpin-project",
      version: 1,
      title: "demo",
      createdAt: "2026-08-11T00:00:00.000Z",
      updatedAt: "2026-08-11T00:00:00.000Z",
      image: { dataUrl: "data:image/png;base64,AA==", width: 100, height: 80, name: "demo" },
      annotations: [annotation],
    };
    expect(isScreenPinProject(project)).toBe(true);
    expect(isScreenPinProject({ ...project, version: 2 })).toBe(false);
    expect(isScreenPinProject({ ...project, annotations: [{ ...annotation, points: "invalid" }] })).toBe(false);
    expect(isScreenPinProject({ ...project, annotations: [{ ...annotation, opacity: 2 }] })).toBe(false);
    expect(isScreenPinProject({ ...project, annotations: [annotation, { ...annotation }] })).toBe(false);
    expect(isScreenPinProject({ ...project, annotations: [{ ...annotation, type: "mosaic", opacity: 0.5 }] })).toBe(false);
  });

  it("accepts an editable area highlight", () => {
    const areaHighlight: Annotation = {
      ...annotation,
      id: "highlight-1",
      type: "area-highlight",
      color: "#f2c94c",
      opacity: 0.35,
    };
    const project: ScreenPinProject = {
      format: "screenpin-project",
      version: 1,
      title: "highlight",
      createdAt: "2026-08-11T00:00:00.000Z",
      updatedAt: "2026-08-11T00:00:00.000Z",
      image: { dataUrl: "data:image/png;base64,AA==", width: 100, height: 80, name: "demo" },
      annotations: [areaHighlight],
    };
    expect(isScreenPinProject(project)).toBe(true);
  });
});

describe("export formats", () => {
  it("maps formats to canvas mime types", () => {
    expect(mimeForFormat("png")).toBe("image/png");
    expect(mimeForFormat("jpeg")).toBe("image/jpeg");
    expect(mimeForFormat("webp")).toBe("image/webp");
  });

  it("uses the conventional jpg extension for jpeg", () => {
    expect(extensionForFormat("png")).toBe("png");
    expect(extensionForFormat("jpeg")).toBe("jpg");
    expect(extensionForFormat("webp")).toBe("webp");
  });

  it("only offers a quality slider for lossy formats", () => {
    expect(isLossyFormat("png")).toBe(false);
    expect(isLossyFormat("jpeg")).toBe(true);
    expect(isLossyFormat("webp")).toBe(true);
  });
});
