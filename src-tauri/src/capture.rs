use std::fs;

use tauri::{State, WebviewWindow};

use crate::{
    media::{png_data_url, CaptureResult},
    window::{active_monitor, cursor_monitor, show_editor_window_inner, CaptureState},
};

#[cfg(target_os = "macos")]
use std::{
    path::PathBuf,
    process::Command,
    time::{SystemTime, UNIX_EPOCH},
};

#[cfg(target_os = "windows")]
use {
    arboard::Clipboard,
    image::{DynamicImage, ImageFormat, RgbaImage},
    std::{io::Cursor, process::Command},
};

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
    let before = clipboard_sequence_number();
    Command::new("explorer.exe")
        .arg("ms-screenclip:")
        .spawn()
        .map_err(|error| format!("无法启动系统截图: {error}"))?;
    for _ in 0..300 {
        std::thread::sleep(std::time::Duration::from_millis(100));
        if clipboard_sequence_number() == before {
            continue;
        }
        if let Some(bytes) = clipboard_image() {
            let image = RgbaImage::from_raw(bytes.0 as u32, bytes.1 as u32, bytes.2)
                .ok_or_else(|| "Windows 截图数据无效".to_string())?;
            let mut png = Cursor::new(Vec::new());
            DynamicImage::ImageRgba8(image)
                .write_to(&mut png, ImageFormat::Png)
                .map_err(|error| format!("无法编码 Windows 截图: {error}"))?;
            return Ok(png.into_inner());
        }
    }

    Err("截图已取消或等待超时".to_string())
}

#[cfg(target_os = "windows")]
fn clipboard_sequence_number() -> u32 {
    #[link(name = "user32")]
    extern "system" {
        fn GetClipboardSequenceNumber() -> u32;
    }

    // The sequence number changes even when new clipboard pixels are identical
    // to the previous screenshot, unlike a content fingerprint.
    unsafe { GetClipboardSequenceNumber() }
}

#[cfg(target_os = "windows")]
fn clipboard_image() -> Option<(usize, usize, Vec<u8>)> {
    let mut clipboard = Clipboard::new().ok()?;
    let image = clipboard.get_image().ok()?;
    Some((image.width, image.height, image.bytes.into_owned()))
}

#[cfg(target_os = "macos")]
fn temporary_capture_path() -> PathBuf {
    let nonce = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap_or_default()
        .as_nanos();
    std::env::temp_dir().join(format!("screenpin-{nonce}.png"))
}

#[tauri::command]
pub async fn capture_screen(
    window: WebviewWindow,
    capture: State<'_, CaptureState>,
) -> Result<CaptureResult, String> {
    // Resolve a fallback before hiding the window. After the system selector
    // closes, the pointer is usually on the selected screen; on Wayland the
    // global pointer is unavailable, so the window's current screen is the
    // best fallback.
    let fallback_target = cursor_monitor(&window)
        .or_else(|| active_monitor(&window).ok())
        .map(|monitor| *monitor.position());
    let _ = window.hide();
    std::thread::sleep(std::time::Duration::from_millis(180));
    let result = native_capture()
        .await
        .and_then(|bytes| png_data_url(&bytes));
    match result {
        Ok(result) => {
            let target = cursor_monitor(&window)
                .map(|monitor| *monitor.position())
                .or(fallback_target);
            capture.remember_target(target)?;
            // Keep the window hidden until the frontend has rendered the new
            // image and fitted its geometry. Showing it here lets the window
            // manager restore the pre-capture position after our fit call.
            Ok(result)
        }
        Err(error) => {
            let _ = capture.clear_target();
            // A cancelled/failed capture must leave the editor usable.
            let _ = show_editor_window_inner(&window);
            Err(error)
        }
    }
}
