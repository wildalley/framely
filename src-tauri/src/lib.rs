use std::{borrow::Cow, fs, io::Cursor, path::Path};

use arboard::{Clipboard, ImageData};
use base64::{engine::general_purpose::STANDARD, Engine as _};
use image::{DynamicImage, GenericImageView, ImageFormat, RgbaImage};
use serde::Serialize;
use tauri::{
    menu::{Menu, MenuItem, PredefinedMenuItem},
    tray::{MouseButton, MouseButtonState, TrayIconBuilder, TrayIconEvent},
    AppHandle, Emitter, Manager, Monitor, PhysicalPosition, PhysicalSize, WebviewWindow,
    WindowEvent,
};
use tauri_plugin_global_shortcut::{GlobalShortcutExt, ShortcutState};

#[cfg(any(target_os = "macos", target_os = "windows"))]
use std::{
    path::PathBuf,
    process::Command,
    time::{SystemTime, UNIX_EPOCH},
};

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
struct CaptureResult {
    data_url: String,
    width: u32,
    height: u32,
}

fn png_data_url(bytes: &[u8]) -> Result<CaptureResult, String> {
    let image = image::load_from_memory(bytes).map_err(|error| format!("无法读取截图: {error}"))?;
    let (width, height) = image.dimensions();
    let mut png = Cursor::new(Vec::new());
    image
        .write_to(&mut png, ImageFormat::Png)
        .map_err(|error| format!("无法编码截图: {error}"))?;

    Ok(CaptureResult {
        data_url: format!(
            "data:image/png;base64,{}",
            STANDARD.encode(png.into_inner())
        ),
        width,
        height,
    })
}

fn decode_data_url(data_url: &str) -> Result<Vec<u8>, String> {
    let (_, encoded) = data_url
        .split_once(',')
        .ok_or_else(|| "图片数据格式无效".to_string())?;
    STANDARD
        .decode(encoded)
        .map_err(|error| format!("图片数据解码失败: {error}"))
}

#[cfg(target_os = "linux")]
async fn native_capture() -> Result<Vec<u8>, String> {
    use ashpd::desktop::screenshot::Screenshot;

    let response = Screenshot::request()
        .interactive(true)
        .modal(true)
        .send()
        .await
        .map_err(|error| format!("截图门户不可用: {error}"))?
        .response()
        .map_err(|error| format!("截图已取消或失败: {error}"))?;
    let url = url::Url::parse(response.uri().as_str())
        .map_err(|error| format!("截图地址无效: {error}"))?;
    let path = url
        .to_file_path()
        .map_err(|_| "截图门户返回的不是本地文件".to_string())?;
    let bytes = fs::read(&path).map_err(|error| format!("无法读取截图文件: {error}"));
    // The portal hands the caller a temporary local file. Do not leave captured
    // pixels behind after importing them into the editor.
    let _ = fs::remove_file(path);
    bytes
}

#[cfg(target_os = "macos")]
async fn native_capture() -> Result<Vec<u8>, String> {
    let path = temporary_capture_path();
    let status = Command::new("screencapture")
        .args(["-i", "-x"])
        .arg(&path)
        .status()
        .map_err(|error| format!("无法启动系统截图: {error}"))?;
    if !status.success() || !path.exists() {
        let _ = fs::remove_file(&path);
        return Err("截图已取消".to_string());
    }
    let bytes = fs::read(&path).map_err(|error| format!("无法读取截图: {error}"));
    let _ = fs::remove_file(path);
    bytes
}

#[cfg(target_os = "windows")]
async fn native_capture() -> Result<Vec<u8>, String> {
    let before = clipboard_fingerprint();
    let _ = Command::new("explorer.exe").arg("ms-screenclip:").spawn();
    for _ in 0..300 {
        std::thread::sleep(std::time::Duration::from_millis(100));
        if let Some((fingerprint, bytes)) = clipboard_image_with_fingerprint() {
            if before.as_ref() != Some(&fingerprint) {
                let image = RgbaImage::from_raw(bytes.0 as u32, bytes.1 as u32, bytes.2)
                    .ok_or_else(|| "Windows 截图数据无效".to_string())?;
                let mut png = Cursor::new(Vec::new());
                DynamicImage::ImageRgba8(image)
                    .write_to(&mut png, ImageFormat::Png)
                    .map_err(|error| format!("无法编码 Windows 截图: {error}"))?;
                return Ok(png.into_inner());
            }
        }
    }

    capture_windows_virtual_screen()
}

#[cfg(target_os = "windows")]
fn capture_windows_virtual_screen() -> Result<Vec<u8>, String> {
    let path = temporary_capture_path();
    let output_path = path.to_string_lossy().replace('\'', "''");
    let script = format!(
        "Add-Type -AssemblyName System.Windows.Forms; Add-Type -AssemblyName System.Drawing; \
         $r=[System.Windows.Forms.SystemInformation]::VirtualScreen; \
         $b=New-Object System.Drawing.Bitmap $r.Width,$r.Height; \
         $g=[System.Drawing.Graphics]::FromImage($b); \
         $g.CopyFromScreen($r.Left,$r.Top,0,0,$b.Size); \
         $b.Save('{output_path}',[System.Drawing.Imaging.ImageFormat]::Png); \
         $g.Dispose(); $b.Dispose();"
    );
    let status = Command::new("powershell")
        .args(["-NoProfile", "-NonInteractive", "-Command", &script])
        .status()
        .map_err(|error| format!("无法启动系统截图: {error}"))?;
    if !status.success() || !path.exists() {
        let _ = fs::remove_file(&path);
        return Err("Windows 全屏截图失败".to_string());
    }
    let bytes = fs::read(&path).map_err(|error| format!("无法读取截图: {error}"));
    let _ = fs::remove_file(path);
    bytes
}

#[cfg(target_os = "windows")]
fn clipboard_image_with_fingerprint() -> Option<((usize, usize, u64), (usize, usize, Vec<u8>))> {
    use std::hash::{Hash, Hasher};
    let mut clipboard = Clipboard::new().ok()?;
    let image = clipboard.get_image().ok()?;
    let bytes = image.bytes.into_owned();
    let mut hasher = std::collections::hash_map::DefaultHasher::new();
    bytes.hash(&mut hasher);
    Some((
        (image.width, image.height, hasher.finish()),
        (image.width, image.height, bytes),
    ))
}

#[cfg(target_os = "windows")]
fn clipboard_fingerprint() -> Option<(usize, usize, u64)> {
    clipboard_image_with_fingerprint().map(|(fingerprint, _)| fingerprint)
}

#[cfg(any(target_os = "macos", target_os = "windows"))]
fn temporary_capture_path() -> PathBuf {
    let nonce = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap_or_default()
        .as_nanos();
    std::env::temp_dir().join(format!("screenpin-{nonce}.png"))
}

#[tauri::command]
async fn capture_screen(window: WebviewWindow) -> Result<CaptureResult, String> {
    let _ = window.hide();
    std::thread::sleep(std::time::Duration::from_millis(180));
    let result = native_capture()
        .await
        .and_then(|bytes| png_data_url(&bytes));
    let _ = window.show();
    let _ = window.set_focus();
    result
}

#[tauri::command]
fn read_clipboard_image() -> Result<CaptureResult, String> {
    let mut clipboard = Clipboard::new().map_err(|error| format!("无法访问剪贴板: {error}"))?;
    let image = clipboard
        .get_image()
        .map_err(|_| "剪贴板中没有图片".to_string())?;
    let rgba = RgbaImage::from_raw(
        image.width as u32,
        image.height as u32,
        image.bytes.into_owned(),
    )
    .ok_or_else(|| "剪贴板图片数据无效".to_string())?;
    let mut png = Cursor::new(Vec::new());
    DynamicImage::ImageRgba8(rgba)
        .write_to(&mut png, ImageFormat::Png)
        .map_err(|error| format!("无法编码剪贴板图片: {error}"))?;
    png_data_url(&png.into_inner())
}

#[tauri::command]
fn copy_image(data_url: String) -> Result<(), String> {
    let bytes = decode_data_url(&data_url)?;
    let image = image::load_from_memory(&bytes)
        .map_err(|error| format!("无法读取导出图片: {error}"))?
        .into_rgba8();
    let (width, height) = image.dimensions();
    let mut clipboard = Clipboard::new().map_err(|error| format!("无法访问剪贴板: {error}"))?;
    clipboard
        .set_image(ImageData {
            width: width as usize,
            height: height as usize,
            bytes: Cow::Owned(image.into_raw()),
        })
        .map_err(|error| format!("复制图片失败: {error}"))
}

#[tauri::command]
fn save_image(path: String, data_url: String) -> Result<(), String> {
    let bytes = decode_data_url(&data_url)?;
    fs::write(Path::new(&path), bytes).map_err(|error| format!("保存图片失败: {error}"))
}

#[tauri::command]
fn write_project(path: String, contents: String) -> Result<(), String> {
    fs::write(Path::new(&path), contents).map_err(|error| format!("保存工程失败: {error}"))
}

#[tauri::command]
fn read_project(path: String) -> Result<String, String> {
    fs::read_to_string(Path::new(&path)).map_err(|error| format!("读取工程失败: {error}"))
}

#[tauri::command]
fn set_main_pinned(window: WebviewWindow, pinned: bool) -> Result<(), String> {
    window
        .set_always_on_top(pinned)
        .map_err(|error| format!("切换窗口置顶失败: {error}"))
}

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
struct WorkArea {
    width: f64,
    height: f64,
}

fn active_monitor(window: &WebviewWindow) -> Result<Monitor, String> {
    let monitor = window
        .current_monitor()
        .or_else(|_| window.primary_monitor())
        .map_err(|error| format!("无法读取显示器信息: {error}"))?;
    monitor.ok_or_else(|| "没有可用的显示器".to_string())
}

/// The usable desktop area in logical pixels, so the editor can pick a zoom
/// level that keeps the whole screenshot on screen.
#[tauri::command]
fn work_area(window: WebviewWindow) -> Result<WorkArea, String> {
    let monitor = active_monitor(&window)?;
    let scale = monitor.scale_factor();
    let area = monitor.work_area();
    Ok(WorkArea {
        width: f64::from(area.size.width) / scale,
        height: f64::from(area.size.height) / scale,
    })
}

/// Resize the frameless window to hug the screenshot plus its toolbar, keeping
/// it inside the work area.
#[tauri::command]
fn fit_window(window: WebviewWindow, width: f64, height: f64, center: bool) -> Result<(), String> {
    let monitor = active_monitor(&window)?;
    let scale = monitor.scale_factor();
    let area = *monitor.work_area();

    let physical = PhysicalSize::new(
        (width * scale).round().max(1.0) as u32,
        (height * scale).round().max(1.0) as u32,
    );
    let size = PhysicalSize::new(
        physical.width.min(area.size.width),
        physical.height.min(area.size.height),
    );
    window
        .set_size(size)
        .map_err(|error| format!("调整窗口尺寸失败: {error}"))?;

    let position = if center {
        PhysicalPosition::new(
            area.position.x + (area.size.width as i32 - size.width as i32) / 2,
            area.position.y + (area.size.height as i32 - size.height as i32) / 2,
        )
    } else {
        let current = window
            .outer_position()
            .map_err(|error| format!("无法读取窗口位置: {error}"))?;
        PhysicalPosition::new(
            current
                .x
                .min(area.position.x + area.size.width as i32 - size.width as i32)
                .max(area.position.x),
            current
                .y
                .min(area.position.y + area.size.height as i32 - size.height as i32)
                .max(area.position.y),
        )
    };
    window
        .set_position(position)
        .map_err(|error| format!("移动窗口失败: {error}"))
}

/// Frameless windows have no title bar, so dragging is driven from the toolbar.
#[tauri::command]
fn start_drag(window: WebviewWindow) -> Result<(), String> {
    window
        .start_dragging()
        .map_err(|error| format!("拖动窗口失败: {error}"))
}

#[tauri::command]
fn hide_window(window: WebviewWindow) -> Result<(), String> {
    window
        .hide()
        .map_err(|error| format!("隐藏窗口失败: {error}"))
}

#[cfg(target_os = "linux")]
fn configure_linux_webview_backend() {
    if let Some(backend) = std::env::var_os("SCREENPIN_GDK_BACKEND") {
        std::env::set_var("GDK_BACKEND", backend);
    } else if std::env::var("XDG_SESSION_TYPE").as_deref() == Ok("wayland")
        && std::env::var_os("DISPLAY").is_some()
    {
        // WebKitGTK can terminate with GDK protocol error 71 on some Wayland
        // compositors. The screenshot itself still uses the Wayland portal.
        std::env::set_var("GDK_BACKEND", "x11");
    }

    if std::env::var_os("WEBKIT_DISABLE_DMABUF_RENDERER").is_none() {
        std::env::set_var("WEBKIT_DISABLE_DMABUF_RENDERER", "1");
    }
}

fn show_main_window(app: &AppHandle) {
    if let Some(window) = app.get_webview_window("main") {
        let _ = window.show();
        let _ = window.unminimize();
        let _ = window.set_focus();
    }
}

fn setup_tray(app: &tauri::App) -> tauri::Result<()> {
    let capture = MenuItem::with_id(app, "capture", "截图", true, None::<&str>)?;
    let show = MenuItem::with_id(app, "show", "显示 ScreenPin", true, None::<&str>)?;
    let separator = PredefinedMenuItem::separator(app)?;
    let quit = MenuItem::with_id(app, "quit", "退出", true, None::<&str>)?;
    let menu = Menu::with_items(app, &[&capture, &show, &separator, &quit])?;

    let mut tray = TrayIconBuilder::with_id("screenpin-tray")
        .menu(&menu)
        .show_menu_on_left_click(false)
        .tooltip("ScreenPin");
    if let Some(icon) = app.default_window_icon().cloned() {
        tray = tray.icon(icon);
    }

    tray.on_menu_event(|app, event| {
        if event.id() == "capture" {
            let _ = app.emit("capture-requested", ());
        } else if event.id() == "show" {
            show_main_window(app);
        } else if event.id() == "quit" {
            app.exit(0);
        }
    })
    .on_tray_icon_event(|tray, event| {
        if let TrayIconEvent::Click {
            button: MouseButton::Left,
            button_state: MouseButtonState::Up,
            ..
        } = event
        {
            show_main_window(tray.app_handle());
        }
    })
    .build(app)?;

    Ok(())
}

#[cfg(target_os = "linux")]
async fn run_wayland_shortcut(app: AppHandle) -> Result<(), String> {
    use ashpd::desktop::{
        global_shortcuts::{BindShortcutsOptions, GlobalShortcuts, NewShortcut},
        CreateSessionOptions,
    };
    use futures_util::StreamExt;

    let portal = GlobalShortcuts::new()
        .await
        .map_err(|error| format!("全局快捷键门户不可用: {error}"))?;
    let mut activated = portal
        .receive_activated()
        .await
        .map_err(|error| format!("无法监听全局快捷键: {error}"))?;
    let session = portal
        .create_session(CreateSessionOptions::default())
        .await
        .map_err(|error| format!("无法创建全局快捷键会话: {error}"))?;
    let shortcut = NewShortcut::new("capture", "截取屏幕").preferred_trigger(Some("CTRL+SHIFT+A"));
    let response = portal
        .bind_shortcuts(&session, &[shortcut], None, BindShortcutsOptions::default())
        .await
        .map_err(|error| format!("无法请求全局快捷键授权: {error}"))?
        .response()
        .map_err(|error| format!("全局快捷键未授权: {error}"))?;

    if !response
        .shortcuts()
        .iter()
        .any(|shortcut| shortcut.id() == "capture")
    {
        return Err("桌面门户没有绑定截图快捷键".to_string());
    }

    eprintln!("ScreenPin: Wayland global shortcut registered");
    while let Some(event) = activated.next().await {
        if event.shortcut_id() == "capture" {
            let _ = app.emit("capture-requested", ());
        }
    }

    Err("全局快捷键门户会话已结束".to_string())
}

fn setup_global_shortcut(app: &tauri::App) -> Result<(), Box<dyn std::error::Error>> {
    #[cfg(target_os = "linux")]
    if std::env::var("XDG_SESSION_TYPE").as_deref() == Ok("wayland") {
        let app_handle = app.handle().clone();
        tauri::async_runtime::spawn(async move {
            if let Err(error) = run_wayland_shortcut(app_handle.clone()).await {
                eprintln!("ScreenPin: {error}; falling back to X11 shortcut registration");
                if let Err(error) = app_handle
                    .global_shortcut()
                    .register("CommandOrControl+Shift+A")
                {
                    eprintln!("ScreenPin: fallback shortcut registration failed: {error}");
                }
            }
        });
        return Ok(());
    }

    app.global_shortcut().register("CommandOrControl+Shift+A")?;
    Ok(())
}

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    #[cfg(target_os = "linux")]
    configure_linux_webview_backend();

    tauri::Builder::default()
        .plugin(tauri_plugin_dialog::init())
        .plugin(
            tauri_plugin_global_shortcut::Builder::new()
                .with_handler(move |app, _shortcut, event| {
                    if event.state() == ShortcutState::Pressed {
                        let _ = app.emit("capture-requested", ());
                    }
                })
                .build(),
        )
        .setup(|app| {
            setup_tray(app)?;
            setup_global_shortcut(app)?;
            Ok(())
        })
        .on_window_event(|window, event| {
            if window.label() == "main" {
                if let WindowEvent::CloseRequested { api, .. } = event {
                    api.prevent_close();
                    let _ = window.hide();
                }
            }
        })
        .invoke_handler(tauri::generate_handler![
            capture_screen,
            read_clipboard_image,
            copy_image,
            save_image,
            write_project,
            read_project,
            set_main_pinned,
            work_area,
            fit_window,
            start_drag,
            hide_window
        ])
        .run(tauri::generate_context!())
        .expect("failed to run ScreenPin");
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn decodes_a_data_url() {
        let bytes = decode_data_url("data:image/png;base64,aGVsbG8=").unwrap();
        assert_eq!(bytes, b"hello");
    }
}
