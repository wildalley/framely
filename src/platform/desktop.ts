import { invoke } from "@tauri-apps/api/core";
import { confirm } from "@tauri-apps/plugin-dialog";
import type { CaptureResult } from "../types";

export interface AppConfig {
  captureShortcut: string;
}

export const isTauri = () => "__TAURI_INTERNALS__" in window;

export const captureScreen = () => invoke<CaptureResult>("capture_screen");

export const pasteImage = () => invoke<CaptureResult>("read_clipboard_image");

export const copyImage = (dataUrl: string) => invoke<void>("copy_image", { dataUrl });

export const setMainPinned = (pinned: boolean) => invoke<void>("set_main_pinned", { pinned });

export const workArea = () => invoke<{ width: number; height: number }>("work_area");

export const fitWindow = (width: number, height: number, center: boolean) =>
  invoke<void>("fit_window", { width, height, center });

export const startWindowDrag = () => invoke<void>("start_drag");

export const hideWindow = () => invoke<void>("hide_window");

export const showMainWindow = () => invoke<void>("show_editor_window");

export const quitApp = () => invoke<void>("quit_app");

export const getConfig = () => invoke<AppConfig>("get_config");

export const saveConfig = (config: AppConfig) => invoke<void>("save_config", { newConfig: config });

export const confirmDiscardChanges = () =>
  isTauri()
    ? confirm("当前工程有未保存的修改，继续将丢失这些修改。", {
        title: "放弃未保存的修改？",
        kind: "warning",
        okLabel: "继续",
        cancelLabel: "取消",
      })
    : Promise.resolve(window.confirm("当前工程有未保存的修改，继续将丢失这些修改。"));
