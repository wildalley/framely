import { describe, expect, it } from "vitest";
import {
  extensionForFormat,
  isDocumentDirty,
  isLossyFormat,
  isScreenPinProject,
  mimeForFormat,
  normalizeBox,
  sameAnnotations,
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

describe("isDocumentDirty", () => {
  const image = { dataUrl: "data:image/png;base64,AA==", width: 100, height: 80, name: "demo" };
  const saved = {
    imageDataUrl: image.dataUrl,
    title: "demo",
    createdAt: "2026-08-11T00:00:00.000Z",
    annotations: JSON.stringify([annotation]),
  };

  it("treats a new capture as unsaved and an empty editor as clean", () => {
    expect(isDocumentDirty(null, "demo", saved.createdAt, [], null)).toBe(false);
    expect(isDocumentDirty(image, "demo", saved.createdAt, [], null)).toBe(true);
  });

  it("keeps an untouched capture clean against its baseline until it is edited", () => {
    const baseline = { ...saved, annotations: JSON.stringify([]) };
    expect(isDocumentDirty(image, "demo", saved.createdAt, [], baseline)).toBe(false);
    expect(isDocumentDirty(image, "demo", saved.createdAt, [annotation], baseline)).toBe(true);
  });

  it("tracks editable project fields against the saved snapshot", () => {
    expect(isDocumentDirty(image, "demo", saved.createdAt, [annotation], saved)).toBe(false);
    expect(isDocumentDirty(image, "renamed", saved.createdAt, [annotation], saved)).toBe(true);
    expect(isDocumentDirty(image, "demo", saved.createdAt, [{ ...annotation, x: 101 }], saved)).toBe(true);
  });
});

describe("sameAnnotations", () => {
  const stroke: Annotation = {
    ...annotation,
    id: "pen-1",
    type: "pen",
    points: [
      { x: 1, y: 2 },
      { x: 3, y: 4 },
    ],
  };

  it("treats the same contents as unchanged", () => {
    expect(sameAnnotations([annotation], [annotation])).toBe(true);
    expect(sameAnnotations([annotation], [{ ...annotation }])).toBe(true);
    expect(sameAnnotations([], [])).toBe(true);
  });

  it("notices edits, additions and removals", () => {
    expect(sameAnnotations([annotation], [{ ...annotation, x: 101 }])).toBe(false);
    expect(sameAnnotations([annotation], [])).toBe(false);
    expect(sameAnnotations([annotation], [annotation, stroke])).toBe(false);
    expect(sameAnnotations([annotation, stroke], [stroke, annotation])).toBe(false);
  });

  it("compares stroke points, including a growing stroke", () => {
    expect(sameAnnotations([stroke], [{ ...stroke, points: [...stroke.points!] }])).toBe(true);
    expect(sameAnnotations([stroke], [{ ...stroke, points: [...stroke.points!, { x: 5, y: 6 }] }])).toBe(false);
    expect(sameAnnotations([stroke], [{ ...stroke, points: [{ x: 1, y: 2 }, { x: 3, y: 9 }] }])).toBe(false);
  });

  // Key order and absent-vs-undefined both change JSON.stringify output but
  // not the annotation, so a field compare is the more accurate answer here.
  it("ignores property order and absent optional fields", () => {
    const reordered = Object.fromEntries(Object.entries(annotation).reverse()) as Annotation;
    expect(sameAnnotations([annotation], [reordered])).toBe(true);
    expect(sameAnnotations([annotation], [{ ...annotation, endX: undefined }])).toBe(true);
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
