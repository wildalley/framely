import { invoke } from "@tauri-apps/api/core";
import { open, save } from "@tauri-apps/plugin-dialog";
import type { CaptureResult, ExportFormat, ScreenPinProject } from "./types";
import { EXPORT_FORMATS, extensionForFormat } from "./types";

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

export const getConfig = () => invoke<AppConfig>("get_config");

export const saveConfig = (config: AppConfig) => invoke<void>("save_config", { newConfig: config });

export async function saveImageAs(dataUrl: string, baseName: string, format: ExportFormat): Promise<boolean> {
  const extension = extensionForFormat(format);
  const suggestedName = `${safeFileName(baseName)}.${extension}`;
  if (!isTauri()) {
    download(dataUrl, suggestedName);
    return true;
  }
  const label = EXPORT_FORMATS.find((entry) => entry.id === format)?.label ?? format.toUpperCase();
  const path = await save({
    defaultPath: suggestedName,
    filters: [{ name: `${label} 图片`, extensions: [extension] }],
  });
  if (!path) return false;
  await invoke("save_image", { path, dataUrl });
  return true;
}

export async function saveProject(project: ScreenPinProject): Promise<boolean> {
  const contents = JSON.stringify(project);
  const fileName = `${safeFileName(project.title)}.screenpin`;
  if (!isTauri()) {
    download(`data:application/json;charset=utf-8,${encodeURIComponent(contents)}`, fileName);
    return true;
  }
  const path = await save({
    defaultPath: fileName,
    filters: [{ name: "ScreenPin 工程", extensions: ["screenpin"] }],
  });
  if (!path) return false;
  await invoke("write_project", { path, contents });
  return true;
}

export async function openProject(): Promise<ScreenPinProject | null> {
  if (!isTauri()) return null;
  const path = await open({
    multiple: false,
    directory: false,
    filters: [{ name: "ScreenPin 工程", extensions: ["screenpin"] }],
  });
  if (!path || Array.isArray(path)) return null;
  const contents = await invoke<string>("read_project", { path });
  return JSON.parse(contents) as ScreenPinProject;
}

export function imageFromFile(file: File): Promise<CaptureResult> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onerror = () => reject(new Error("无法读取图片"));
    reader.onload = () => {
      const dataUrl = String(reader.result);
      const image = new Image();
      image.onerror = () => reject(new Error("不支持的图片格式"));
      image.onload = () => resolve({ dataUrl, width: image.naturalWidth, height: image.naturalHeight });
      image.src = dataUrl;
    };
    reader.readAsDataURL(file);
  });
}

export function pixelateImage(dataUrl: string, width: number, height: number): Promise<string> {
  return new Promise((resolve, reject) => {
    const image = new Image();
    image.onerror = () => reject(new Error("无法生成马赛克预览"));
    image.onload = () => {
      if (image.naturalWidth !== width || image.naturalHeight !== height) {
        reject(new Error("图片尺寸与工程数据不一致"));
        return;
      }
      const block = Math.max(8, Math.round(Math.min(width, height) / 90));
      const small = document.createElement("canvas");
      small.width = Math.max(1, Math.ceil(width / block));
      small.height = Math.max(1, Math.ceil(height / block));
      const context = small.getContext("2d");
      if (!context) return reject(new Error("浏览器画布不可用"));
      context.drawImage(image, 0, 0, small.width, small.height);
      resolve(small.toDataURL("image/png"));
    };
    image.src = dataUrl;
  });
}

export function createDemoImage(): CaptureResult {
  const canvas = document.createElement("canvas");
  canvas.width = 1280;
  canvas.height = 760;
  const context = canvas.getContext("2d")!;
  context.fillStyle = "#f2f3f5";
  context.fillRect(0, 0, canvas.width, canvas.height);
  context.fillStyle = "#202326";
  context.fillRect(0, 0, canvas.width, 64);
  context.fillStyle = "#ffffff";
  context.font = "600 20px sans-serif";
  context.fillText("产品工作台", 32, 40);
  context.fillStyle = "#ffffff";
  context.fillRect(24, 88, 260, 648);
  context.fillRect(308, 88, 948, 116);
  context.fillRect(308, 228, 600, 508);
  context.fillRect(932, 228, 324, 508);
  context.fillStyle = "#1f9d73";
  context.fillRect(336, 124, 168, 44);
  context.fillStyle = "#2f3438";
  context.font = "700 27px sans-serif";
  context.fillText("季度交付概览", 336, 282);
  context.fillStyle = "#778087";
  context.font = "16px sans-serif";
  context.fillText("最近 30 天 · 全部项目", 336, 316);
  [0.68, 0.44, 0.82, 0.58, 0.9, 0.72].forEach((value, index) => {
    context.fillStyle = index === 4 ? "#1f9d73" : "#cdd3d6";
    context.fillRect(352 + index * 82, 650 - value * 260, 46, value * 260);
  });
  return { dataUrl: canvas.toDataURL("image/png"), width: 1280, height: 760 };
}

const safeFileName = (name: string) => name.replace(/[\\/:*?"<>|]/g, "-").trim() || "screenpin";

const download = (href: string, name: string) => {
  const anchor = document.createElement("a");
  anchor.href = href;
  anchor.download = name;
  anchor.click();
};
