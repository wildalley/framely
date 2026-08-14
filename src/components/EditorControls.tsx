import { Check, X } from "lucide-react";
import type { LucideIcon } from "lucide-react";
import type { ToolItem } from "../editor/constants";

interface IconButtonProps {
  icon: LucideIcon;
  label: string;
  active?: boolean;
  disabled?: boolean;
  onClick: () => void;
}

export function IconButton({ icon: Icon, label, active, disabled, onClick }: IconButtonProps) {
  return (
    <button
      className={`icon-button ${active ? "is-active" : ""}`}
      title={label}
      aria-label={label}
      aria-pressed={active}
      disabled={disabled}
      onClick={onClick}
    >
      <Icon size={17} />
    </button>
  );
}

interface ToolButtonProps {
  item: ToolItem;
  active: boolean;
  disabled?: boolean;
  disabledReason?: string;
  onClick: () => void;
}

export function ToolButton({ item, active, disabled, disabledReason, onClick }: ToolButtonProps) {
  const Icon = item.icon;
  return (
    <button
      className={`tool-button ${active ? "is-active" : ""}`}
      title={disabled ? disabledReason : `${item.label} (${item.key})`}
      aria-label={item.label}
      aria-pressed={active}
      disabled={disabled}
      onClick={onClick}
    >
      <Icon size={17} />
    </button>
  );
}

interface ToastProps {
  message: string;
  onDismiss: () => void;
}

export function Toast({ message, onDismiss }: ToastProps) {
  return (
    <div className="toast">
      <Check size={14} />
      {message}
      <button aria-label="关闭提示" onClick={onDismiss}>
        <X size={13} />
      </button>
    </div>
  );
}
