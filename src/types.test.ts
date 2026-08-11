import { describe, expect, it } from "vitest";
import { isScreenPinProject, normalizeBox, type Annotation, type ScreenPinProject } from "./types";

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
  });
});
