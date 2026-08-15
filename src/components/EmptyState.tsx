import { Aperture, FolderOpen, ImagePlus, ScanLine, X } from "lucide-react";
import type { PointerEvent } from "react";

interface EmptyStateProps {
  busy: boolean;
  onDrag: (event: PointerEvent<HTMLDivElement>) => void;
  onCapture: () => void;
  onOpen: () => void;
  onOpenProject: () => void;
  onClose: () => void;
}

export function EmptyState({ busy, onDrag, onCapture, onOpen, onOpenProject, onClose }: EmptyStateProps) {
  return (
    <div className="empty-state" onPointerDown={onDrag}>
      <button className="empty-close" title="关闭到托盘" aria-label="关闭到托盘" onClick={onClose}>
        <X size={15} />
      </button>
      <div className="empty-symbol"><Aperture size={26} /></div>
      <div className="empty-actions">
        <button className="primary-action large" disabled={busy} onClick={onCapture}><ScanLine size={16} />截取屏幕</button>
        <button className="secondary-action" onClick={onOpen}><ImagePlus size={16} />打开图片</button>
        <button className="secondary-action" onClick={onOpenProject}><FolderOpen size={16} />工程</button>
      </div>
      <div className="shortcut-row"><kbd>Win</kbd><span>+</span><kbd>Shift</kbd><span>+</span><kbd>S</kbd></div>
    </div>
  );
}
