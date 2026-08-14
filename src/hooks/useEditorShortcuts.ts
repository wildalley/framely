import { useEffect, type Dispatch, type SetStateAction } from "react";
import { TOOL_ITEMS, type EditorPanel } from "../editor/constants";
import type { ImageSource, Tool } from "../types";

type MosaicStatus = "loading" | "ready" | "error";

interface UseEditorShortcutsOptions {
  applyZoom: (zoom: number) => void;
  image: ImageSource | null;
  mosaicStatus: MosaicStatus;
  notify: (message: string) => void;
  onCopy: () => Promise<void>;
  onDeleteSelected: () => void;
  onSaveImage: () => Promise<void>;
  onSaveProject: () => Promise<void>;
  panel: EditorPanel;
  redo: () => void;
  setPanel: Dispatch<SetStateAction<EditorPanel>>;
  setSelectedId: Dispatch<SetStateAction<string | null>>;
  setTool: Dispatch<SetStateAction<Tool>>;
  undo: () => void;
  zoom: number;
}

export function useEditorShortcuts({
  applyZoom,
  image,
  mosaicStatus,
  notify,
  onCopy,
  onDeleteSelected,
  onSaveImage,
  onSaveProject,
  panel,
  redo,
  setPanel,
  setSelectedId,
  setTool,
  undo,
  zoom,
}: UseEditorShortcutsOptions) {
  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      const target = event.target as HTMLElement;
      if (["INPUT", "TEXTAREA"].includes(target.tagName)) return;

      const modifier = event.ctrlKey || event.metaKey;
      const key = event.key.toLowerCase();
      if (modifier && key === "z") {
        event.preventDefault();
        event.shiftKey ? redo() : undo();
        return;
      }
      if (modifier && key === "y") {
        event.preventDefault();
        redo();
        return;
      }
      if (modifier && key === "c" && image) {
        event.preventDefault();
        void onCopy();
        return;
      }
      if (modifier && key === "s" && image) {
        event.preventDefault();
        void (event.shiftKey ? onSaveProject() : onSaveImage());
        return;
      }
      if (modifier && (key === "=" || key === "+")) {
        event.preventDefault();
        applyZoom(zoom + 0.1);
        return;
      }
      if (modifier && key === "-") {
        event.preventDefault();
        applyZoom(zoom - 0.1);
        return;
      }
      if (modifier && key === "0") {
        event.preventDefault();
        applyZoom(1);
        return;
      }
      if (modifier) return;

      if (event.key === "Delete" || event.key === "Backspace") onDeleteSelected();
      if (event.key === "Escape") {
        if (panel !== "none") {
          setPanel("none");
          return;
        }
        setSelectedId(null);
        setTool("select");
      }
      if (key === "?") {
        setPanel((current) => (current === "shortcuts" ? "none" : "shortcuts"));
        return;
      }

      const item = TOOL_ITEMS.find((candidate) => candidate.key.toLowerCase() === key);
      if (item?.id === "mosaic" && mosaicStatus !== "ready") {
        notify(mosaicStatus === "loading" ? "马赛克正在准备" : "马赛克当前不可用");
        return;
      }
      if (item) setTool(item.id);
    };

    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [
    applyZoom,
    image,
    mosaicStatus,
    notify,
    onCopy,
    onDeleteSelected,
    onSaveImage,
    onSaveProject,
    panel,
    redo,
    setPanel,
    setSelectedId,
    setTool,
    undo,
    zoom,
  ]);
}
