import type { PointerEvent, RefObject } from "react";
import {
  Copy,
  Download,
  HelpCircle,
  MoreHorizontal,
  Palette,
  Pin,
  PinOff,
  Redo2,
  Settings,
  Undo2,
  X,
} from "lucide-react";
import { isTauri } from "../platform";
import { TOOL_ITEMS, type EditorPanel } from "../editor/constants";
import type { AnnotationStyle, ExportFormat, Tool } from "../types";
import { IconButton, ToolButton } from "./EditorControls";

interface EditorToolbarProps {
  activeStyle: AnnotationStyle | null;
  barRef: RefObject<HTMLDivElement | null>;
  canRedo: boolean;
  canUndo: boolean;
  exportFormat: ExportFormat;
  isPinned: boolean;
  mosaicStatus: "loading" | "ready" | "error";
  onClose: () => void;
  onCopy: () => void;
  onPanelChange: (panel: EditorPanel) => void;
  onPin: () => void;
  onRedo: () => void;
  onSaveImage: () => void;
  onToolChange: (tool: Tool) => void;
  onUndo: () => void;
  onPointerDown: (event: PointerEvent<HTMLDivElement>) => void;
  panel: EditorPanel;
  tool: Tool;
}

export function EditorToolbar({
  activeStyle,
  barRef,
  canRedo,
  canUndo,
  exportFormat,
  isPinned,
  mosaicStatus,
  onClose,
  onCopy,
  onPanelChange,
  onPin,
  onRedo,
  onSaveImage,
  onToolChange,
  onUndo,
  onPointerDown,
  panel,
  tool,
}: EditorToolbarProps) {
  const togglePanel = (target: EditorPanel) => onPanelChange(panel === target ? "none" : target);

  return (
    <div ref={barRef} className="pin-bar" aria-label="标注工具" onPointerDown={onPointerDown}>
      {TOOL_ITEMS.map((item) => (
        <ToolButton
          key={item.id}
          item={item}
          active={tool === item.id}
          disabled={item.id === "mosaic" && mosaicStatus !== "ready"}
          disabledReason={mosaicStatus === "loading" ? "马赛克正在准备" : "马赛克处理失败"}
          onClick={() => {
            onToolChange(item.id);
            if (item.id === "select") onPanelChange("none");
          }}
        />
      ))}
      <span className="bar-divider" />
      <button
        className={`icon-button swatch-button ${panel === "style" ? "is-active" : ""}`}
        title={activeStyle ? "样式" : "选中标注或先选择工具"}
        aria-label="样式"
        disabled={!activeStyle}
        onClick={() => togglePanel("style")}
      >
        <Palette size={17} />
        {activeStyle && <span className="swatch-dot" style={{ backgroundColor: activeStyle.color }} />}
      </button>
      <IconButton icon={Undo2} label="撤销" disabled={!canUndo} onClick={onUndo} />
      <IconButton icon={Redo2} label="重做" disabled={!canRedo} onClick={onRedo} />
      <span className="bar-divider" />
      <IconButton
        icon={HelpCircle}
        label="快捷键 (?)"
        active={panel === "shortcuts"}
        onClick={() => togglePanel("shortcuts")}
      />
      {isTauri() && (
        <IconButton
          icon={Settings}
          label="设置"
          active={panel === "settings"}
          onClick={() => togglePanel("settings")}
        />
      )}
      <IconButton
        icon={isPinned ? PinOff : Pin}
        label={isPinned ? "取消置顶" : "置顶"}
        active={isPinned}
        onClick={onPin}
      />
      <IconButton
        icon={Download}
        label={`导出 ${exportFormat.toUpperCase()} (Ctrl+S)`}
        onClick={onSaveImage}
      />
      <IconButton
        icon={MoreHorizontal}
        label="更多"
        active={panel === "menu"}
        onClick={() => togglePanel("menu")}
      />
      <button className="primary-action" onClick={onCopy}><Copy size={15} />复制</button>
      <IconButton icon={X} label="关闭到托盘" onClick={onClose} />
    </div>
  );
}
