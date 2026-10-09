use std::fs;

#[cfg(target_os = "linux")]
use tauri::Manager;
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

/// Error for a capture the user backed out of. The frontend matches on it to
/// treat the cancel as a no-op rather than a failure.
pub(crate) const CAPTURE_CANCELLED: &str = "截图已取消";

/// Ask the portal for a screenshot and import its temporary file.
#[cfg(target_os = "linux")]
async fn portal_screenshot(interactive: bool) -> Result<Vec<u8>, String> {
    use ashpd::desktop::screenshot::Screenshot;

    crate::shortcuts::register_portal_app().await;
    let response = Screenshot::request()
        .interactive(interactive)
        .modal(interactive)
        .send()
        .await
        .map_err(|error| format!("截图门户不可用: {error}"))?
        .response()
        .map_err(|error| match error {
            ashpd::Error::Response(ashpd::desktop::ResponseError::Cancelled) => {
                CAPTURE_CANCELLED.to_string()
            }
            error => format!("截图失败: {error}"),
        })?;
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

/// The user's recorded answer for silent screenshots, if any.
#[cfg(target_os = "linux")]
async fn screenshot_permission() -> Option<bool> {
    use std::collections::HashMap;

    let connection = ashpd::zbus::Connection::session().await.ok()?;
    let reply = connection
        .call_method(
            Some("org.freedesktop.impl.portal.PermissionStore"),
            "/org/freedesktop/impl/portal/PermissionStore",
            Some("org.freedesktop.impl.portal.PermissionStore"),
            "Lookup",
            &("screenshot", "screenshot"),
        )
        .await
        .ok()?;
    let (permissions, _): (
        HashMap<String, Vec<String>>,
        ashpd::zbus::zvariant::OwnedValue,
    ) = reply.body().deserialize().ok()?;
    let answer = permissions.get(crate::shortcuts::PORTAL_APP_ID)?;
    Some(answer.iter().any(|value| value == "yes"))
}

/// GNOME only lets the focused app show the "allow screenshots" dialog, and
/// the capture normally hides the editor first, so the very first silent
/// request would be refused without ever asking. When nothing is recorded yet,
/// ask once while the window is up; the answer is remembered by the portal.
#[cfg(target_os = "linux")]
async fn ensure_screenshot_permission(window: &WebviewWindow) {
    if !crate::window::cursor_position_is_reliable() || screenshot_permission().await.is_some() {
        return;
    }
    let _ = show_editor_window_inner(window);
    std::thread::sleep(std::time::Duration::from_millis(250));
    // The image from this request is discarded: the editor was on screen.
    if let Err(error) = portal_screenshot(false).await {
        eprintln!("ScreenPin: screenshot permission was not granted: {error}");
    }
}

/// Capture the whole desktop silently and let the user drag a region on
/// ScreenPin's own overlays. The overlays need real window placement, which
/// native Wayland clients do not get; there, and if the silent capture is
/// refused, the portal's own interactive selector is used instead.
#[cfg(target_os = "linux")]
async fn native_capture(
    window: &WebviewWindow,
) -> Result<(Vec<u8>, Option<tauri::PhysicalPosition<i32>>), String> {
    if crate::window::cursor_position_is_reliable() {
        match portal_screenshot(false).await {
            Ok(png) => {
                let selected = crate::selection::select_region(window.app_handle(), png).await?;
                return Ok((selected.png, Some(selected.monitor)));
            }
            Err(error) => {
                eprintln!(
                    "ScreenPin: full-screen capture failed ({error}); using the portal selector"
                );
            }
        }
    }
    portal_screenshot(true).await.map(|png| (png, None))
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
        return Err(CAPTURE_CANCELLED.to_string());
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

/// Whether ScreenPin may take silent screenshots: "granted", "denied",
/// "unknown" (never asked) or "unsupported" (platform without the overlay).
#[tauri::command]
pub async fn screenshot_access() -> String {
    #[cfg(target_os = "linux")]
    if crate::window::cursor_position_is_reliable() {
        return match screenshot_permission().await {
            Some(true) => "granted",
            Some(false) => "denied",
            None => "unknown",
        }
        .to_string();
    }
    "unsupported".to_string()
}

/// Ask for silent screenshot access from the settings panel. The click keeps
/// the editor focused, which is what GNOME needs before it shows the dialog.
#[tauri::command]
pub async fn request_screenshot_access(window: WebviewWindow) -> Result<String, String> {
    #[cfg(target_os = "linux")]
    {
        let _ = window.set_focus();
        if let Err(error) = portal_screenshot(false).await {
            eprintln!("ScreenPin: screenshot permission request failed: {error}");
        }
    }
    #[cfg(not(target_os = "linux"))]
    let _ = window;
    Ok(screenshot_access().await)
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
    // Checked before the permission request, which may show the window itself.
    let was_visible = window.is_visible().unwrap_or(true);
    #[cfg(target_os = "linux")]
    ensure_screenshot_permission(&window).await;
    let _ = window.hide();
    std::thread::sleep(std::time::Duration::from_millis(180));
    #[cfg(target_os = "linux")]
    let captured = native_capture(&window).await;
    #[cfg(not(target_os = "linux"))]
    let captured = native_capture().await.map(|bytes| (bytes, None));
    let result = captured.and_then(|(bytes, monitor)| Ok((png_data_url(&bytes)?, monitor)));
    match result {
        Ok((result, selected_monitor)) => {
            // Open the editor on the screen the region was picked from.
            let target = selected_monitor
                .or_else(|| cursor_monitor(&window).map(|monitor| *monitor.position()))
                .or(fallback_target);
            capture.remember_target(target)?;
            // Keep the window hidden until the frontend has rendered the new
            // image and fitted its geometry. Showing it here lets the window
            // manager restore the pre-capture position after our fit call.
            Ok(result)
        }
        Err(error) => {
            let _ = capture.clear_target();
            // A failed capture must leave the editor usable. A cancelled one
            // just puts things back: started from the tray with the editor
            // hidden, Esc should not pop the old screenshot up.
            if error != CAPTURE_CANCELLED || was_visible {
                let _ = show_editor_window_inner(&window);
            }
            Err(error)
        }
    }
}
