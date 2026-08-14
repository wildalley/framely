use std::sync::Mutex;

use serde::Serialize;
use tauri::{AppHandle, Manager, Monitor, PhysicalPosition, PhysicalSize, State, WebviewWindow};

/// State shared by the capture command and the following editor re-layout.
///
/// A hidden window has no current monitor on several window managers. Keeping
/// the target position here lets `work_area` and `fit_window` use the screen
/// where the capture was made until the editor has been shown again.
#[derive(Default)]
pub struct CaptureState {
    target_monitor: Mutex<Option<PhysicalPosition<i32>>>,
}

impl CaptureState {
    pub(crate) fn remember_target(
        &self,
        position: Option<PhysicalPosition<i32>>,
    ) -> Result<(), String> {
        *self
            .target_monitor
            .lock()
            .map_err(|error| format!("锁定截图状态失败: {error}"))? = position;
        Ok(())
    }

    pub(crate) fn target(&self) -> Option<PhysicalPosition<i32>> {
        self.target_monitor.lock().ok()?.as_ref().copied()
    }

    pub(crate) fn clear_target(&self) -> Result<(), String> {
        self.remember_target(None)
    }
}

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
pub struct WorkArea {
    width: f64,
    height: f64,
}

pub(crate) fn active_monitor(window: &WebviewWindow) -> Result<Monitor, String> {
    // `current_monitor` returns `Ok(None)` while a window is hidden. Treat that
    // the same as an unavailable current monitor and fall back to the primary
    // display instead of failing all subsequent geometry updates.
    let monitor = window.current_monitor().ok().flatten();
    monitor
        .or_else(|| window.primary_monitor().ok().flatten())
        .ok_or_else(|| "没有可用的显示器".to_string())
}

fn monitor_at_position(window: &WebviewWindow, position: PhysicalPosition<i32>) -> Option<Monitor> {
    window
        .monitor_from_point(f64::from(position.x), f64::from(position.y))
        .ok()
        .flatten()
}

pub(crate) fn cursor_monitor(window: &WebviewWindow) -> Option<Monitor> {
    if !cursor_position_is_reliable() {
        return None;
    }
    let cursor = window.cursor_position().ok()?;
    window.monitor_from_point(cursor.x, cursor.y).ok().flatten()
}

#[cfg(target_os = "linux")]
fn cursor_position_is_reliable() -> bool {
    let gdk_backend = std::env::var("GDK_BACKEND").ok();
    let session_type = std::env::var("XDG_SESSION_TYPE").ok();
    match gdk_backend {
        // GDK_BACKEND is a comma-separated priority list; only the first entry
        // is guaranteed to be the backend actually in use.
        Some(backend) => backend.split(',').next() == Some("x11"),
        None => session_type.as_deref() != Some("wayland"),
    }
}

#[cfg(not(target_os = "linux"))]
fn cursor_position_is_reliable() -> bool {
    true
}

fn target_monitor(window: &WebviewWindow, capture: &CaptureState) -> Option<Monitor> {
    capture
        .target()
        .and_then(|position| monitor_at_position(window, position))
}

pub(crate) fn show_editor_window_inner(window: &WebviewWindow) -> Result<(), String> {
    window
        .show()
        .map_err(|error| format!("无法显示主窗口: {error}"))?;
    let _ = window.unminimize();
    window
        .set_focus()
        .map_err(|error| format!("无法聚焦主窗口: {error}"))
}

/// The usable desktop area in logical pixels, so the editor can pick a zoom
/// level that keeps the whole screenshot on screen.
#[tauri::command]
pub fn work_area(
    window: WebviewWindow,
    capture: State<'_, CaptureState>,
) -> Result<WorkArea, String> {
    let monitor = target_monitor(&window, &capture).or_else(|| active_monitor(&window).ok());
    let monitor = monitor.ok_or_else(|| "没有可用的显示器".to_string())?;
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
pub fn fit_window(
    window: WebviewWindow,
    width: f64,
    height: f64,
    center: bool,
    capture: State<'_, CaptureState>,
) -> Result<(), String> {
    // Keep the target available for every hidden re-layout (for example, the
    // toolbar's ResizeObserver may trigger a second fit). It is cleared when
    // `show_editor_window` reveals the settled editor.
    let target = capture.target();
    let monitor = target
        .and_then(|position| monitor_at_position(&window, position))
        .or_else(|| active_monitor(&window).ok())
        .ok_or_else(|| "没有可用的显示器".to_string())?;
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

#[tauri::command]
pub fn set_main_pinned(window: WebviewWindow, pinned: bool) -> Result<(), String> {
    window
        .set_always_on_top(pinned)
        .map_err(|error| format!("切换窗口置顶失败: {error}"))
}

/// Frameless windows have no title bar, so dragging is driven from the toolbar.
#[tauri::command]
pub fn start_drag(window: WebviewWindow) -> Result<(), String> {
    window
        .start_dragging()
        .map_err(|error| format!("拖动窗口失败: {error}"))
}

#[tauri::command]
pub fn hide_window(window: WebviewWindow) -> Result<(), String> {
    window
        .hide()
        .map_err(|error| format!("隐藏窗口失败: {error}"))
}

/// Reveal the editor only after its post-capture geometry has been applied.
#[tauri::command]
pub fn show_editor_window(
    window: WebviewWindow,
    capture: State<'_, CaptureState>,
) -> Result<(), String> {
    show_editor_window_inner(&window)?;
    capture.clear_target()
}

pub(crate) fn show_main_window(app: &AppHandle) {
    let _ = app.state::<CaptureState>().clear_target();
    if let Some(window) = app.get_webview_window("main") {
        let _ = window.show();
        let _ = window.unminimize();
        let _ = window.set_focus();
    }
}
