//! ScreenPin's own region picker for Linux.
//!
//! The portal hands back one image of the whole desktop without its own UI.
//! Each monitor then gets a borderless overlay that shows its slice of that
//! image, and the user drags a region there: releasing confirms, Esc cancels.
//! Only the chosen region is cropped out and sent to the editor.

use std::{
    io::Cursor,
    sync::{mpsc, Mutex},
    time::{Duration, SystemTime, UNIX_EPOCH},
};

use gtk::prelude::{GtkWindowExt, WidgetExt};
use image::ImageFormat;
use serde::{Deserialize, Serialize};
use tauri::{
    ipc::Response, AppHandle, Manager, PhysicalPosition, PhysicalSize, WebviewUrl, WebviewWindow,
    WebviewWindowBuilder,
};

use crate::capture::CAPTURE_CANCELLED;

pub(crate) const OVERLAY_PREFIX: &str = "overlay-";

/// One monitor's place on the desktop and in the captured image.
#[derive(Clone)]
struct Screen {
    label: String,
    /// Physical pixels per GDK unit on this monitor.
    scale: f64,
    position: PhysicalPosition<i32>,
    size: PhysicalSize<u32>,
    crop: Crop,
}

/// A rectangle in captured-image pixels.
#[derive(Clone, Copy, Serialize)]
#[serde(rename_all = "camelCase")]
struct Crop {
    x: u32,
    y: u32,
    width: u32,
    height: u32,
}

struct Session {
    id: String,
    png: Vec<u8>,
    image_size: (u32, u32),
    screens: Vec<Screen>,
    done: Option<mpsc::Sender<Result<Selected, String>>>,
}

/// The cropped PNG plus the monitor it came from, so the editor opens there.
pub(crate) struct Selected {
    pub png: Vec<u8>,
    pub monitor: PhysicalPosition<i32>,
}

#[derive(Default)]
pub struct SelectionState(Mutex<Option<Session>>);

/// Selection rectangle in the overlay's CSS pixels, plus the overlay's size.
#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Region {
    x: f64,
    y: f64,
    width: f64,
    height: f64,
    view_width: f64,
    view_height: f64,
}

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
pub struct OverlayInfo {
    session: String,
    image_width: u32,
    image_height: u32,
    crop: Crop,
}

/// Show the overlays for `png` and wait for the user to pick a region.
pub(crate) async fn select_region(app: &AppHandle, png: Vec<u8>) -> Result<Selected, String> {
    let image_size = image::ImageReader::with_format(Cursor::new(&png), ImageFormat::Png)
        .into_dimensions()
        .map_err(|error| format!("无法读取截图尺寸: {error}"))?;
    let monitors = app
        .available_monitors()
        .map_err(|error| format!("无法读取显示器布局: {error}"))?;
    if monitors.is_empty() {
        return Err("没有可用的显示器".to_string());
    }

    let screens = layout_screens(&monitors, image_size);
    let id = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap_or_default()
        .as_nanos()
        .to_string();
    let (sender, receiver) = mpsc::channel();
    {
        let state = app.state::<SelectionState>();
        let mut session = state
            .0
            .lock()
            .map_err(|error| format!("锁定选区状态失败: {error}"))?;
        if session.is_some() {
            return Err("正在选择截图区域".to_string());
        }
        *session = Some(Session {
            id: id.clone(),
            png,
            image_size,
            screens: screens.clone(),
            done: Some(sender),
        });
    }

    for screen in &screens {
        if let Err(error) = open_overlay(app, screen) {
            finish(app, Err(format!("无法打开选区窗口: {error}")));
            break;
        }
    }

    tauri::async_runtime::spawn_blocking(move || receiver.recv())
        .await
        .map_err(|error| format!("等待选区失败: {error}"))?
        .map_err(|_| CAPTURE_CANCELLED.to_string())?
}

/// Map each monitor onto the image. GNOME's portal returns the bounding box of
/// all monitors; scale it uniformly in case it is not 1:1 with physical pixels.
fn layout_screens(monitors: &[tauri::Monitor], image_size: (u32, u32)) -> Vec<Screen> {
    let left = monitors.iter().map(|m| m.position().x).min().unwrap_or(0);
    let top = monitors.iter().map(|m| m.position().y).min().unwrap_or(0);
    let right = monitors
        .iter()
        .map(|m| m.position().x + m.size().width as i32)
        .max()
        .unwrap_or(1);
    let bottom = monitors
        .iter()
        .map(|m| m.position().y + m.size().height as i32)
        .max()
        .unwrap_or(1);
    let scale_x = f64::from(image_size.0) / f64::from((right - left).max(1));
    let scale_y = f64::from(image_size.1) / f64::from((bottom - top).max(1));

    monitors
        .iter()
        .enumerate()
        .map(|(index, monitor)| {
            let position = *monitor.position();
            let size = *monitor.size();
            let x = (f64::from(position.x - left) * scale_x).round() as u32;
            let y = (f64::from(position.y - top) * scale_y).round() as u32;
            let width = ((f64::from(size.width) * scale_x).round() as u32)
                .min(image_size.0.saturating_sub(x))
                .max(1);
            let height = ((f64::from(size.height) * scale_y).round() as u32)
                .min(image_size.1.saturating_sub(y))
                .max(1);
            Screen {
                label: format!("{OVERLAY_PREFIX}{index}"),
                scale: monitor.scale_factor(),
                position,
                size,
                crop: Crop {
                    x,
                    y,
                    width,
                    height,
                },
            }
        })
        .collect()
}

fn open_overlay(app: &AppHandle, screen: &Screen) -> tauri::Result<()> {
    let window = WebviewWindowBuilder::new(
        app,
        &screen.label,
        WebviewUrl::App("index.html?overlay".into()),
    )
    .title("ScreenPin 选区")
    .decorations(false)
    .resizable(false)
    .skip_taskbar(true)
    .always_on_top(true)
    .visible(false)
    .build()?;
    window.set_position(screen.position)?;
    window.set_size(screen.size)?;
    // The page reveals the overlay once its image is decoded; if it never does,
    // do not leave the capture waiting on an invisible window.
    let app = app.clone();
    let label = screen.label.clone();
    std::thread::spawn(move || {
        std::thread::sleep(Duration::from_secs(3));
        if let Some(window) = app.get_webview_window(&label) {
            if !window.is_visible().unwrap_or(true) {
                let _ = reveal(&app, &window);
            }
        }
    });
    Ok(())
}

fn reveal(app: &AppHandle, window: &WebviewWindow) -> Result<(), String> {
    let screen = screen_for(app, window.label())?;
    // Mutter places new windows on the monitor with the pointer and ignored
    // both the requested position and fullscreen-on-monitor, so every overlay
    // ended up stacked on one screen. Override-redirect windows bypass the
    // window manager: they land exactly where they are put and sit above the
    // top bar. Without the window manager nothing gives them focus, so the
    // overlay under the pointer grabs the keyboard itself for Esc and Enter.
    let grab = app
        .cursor_position()
        .map(|cursor| contains(&screen, cursor))
        .unwrap_or(screen.position == PhysicalPosition::new(0, 0));
    let overlay = window.clone();
    window
        .run_on_main_thread(move || {
            let Ok(gtk_window) = overlay.gtk_window() else {
                return;
            };
            if !gtk_window.is_visible() {
                gtk_window.realize();
                if let Some(gdk_window) = gtk_window.window() {
                    gdk_window.set_override_redirect(true);
                }
            }
            let x = (f64::from(screen.position.x) / screen.scale).round() as i32;
            let y = (f64::from(screen.position.y) / screen.scale).round() as i32;
            let width = (f64::from(screen.size.width) / screen.scale).round() as i32;
            let height = (f64::from(screen.size.height) / screen.scale).round() as i32;
            gtk_window.move_(x, y);
            gtk_window.resize(width, height);
            gtk_window.show_all();
            if let Some(gdk_window) = gtk_window.window() {
                gdk_window.move_resize(x, y, width, height);
                gdk_window.raise();
                if grab {
                    grab_input(&gdk_window, 20);
                }
            }
            let _ = overlay.set_focus();
        })
        .map_err(|error| error.to_string())
}

fn contains(screen: &Screen, point: PhysicalPosition<f64>) -> bool {
    let left = f64::from(screen.position.x);
    let top = f64::from(screen.position.y);
    point.x >= left
        && point.x < left + f64::from(screen.size.width)
        && point.y >= top
        && point.y < top + f64::from(screen.size.height)
}

/// Grab the keyboard (and pointer, with owner events so the other overlays
/// still get their own clicks). A freshly mapped window may not be viewable
/// yet, so retry for a little while.
fn grab_input(window: &gtk::gdk::Window, attempts: u32) {
    use gtk::gdk::prelude::SeatExt;
    let Some(seat) = window.display().default_seat() else {
        return;
    };
    let status = seat.grab(
        window,
        gtk::gdk::SeatCapabilities::ALL,
        true,
        None,
        None,
        None,
    );
    if status != gtk::gdk::GrabStatus::Success && attempts > 0 {
        let window = window.clone();
        gtk::glib::timeout_add_local_once(Duration::from_millis(50), move || {
            grab_input(&window, attempts - 1)
        });
    }
}

fn screen_for(app: &AppHandle, label: &str) -> Result<Screen, String> {
    let state = app.state::<SelectionState>();
    let session = state
        .0
        .lock()
        .map_err(|error| format!("锁定选区状态失败: {error}"))?;
    session
        .as_ref()
        .and_then(|session| session.screens.iter().find(|screen| screen.label == label))
        .cloned()
        .ok_or_else(|| "选区已结束".to_string())
}

/// End the session (once) and close every overlay.
fn finish(app: &AppHandle, result: Result<Selected, String>) {
    let session = app
        .state::<SelectionState>()
        .0
        .lock()
        .ok()
        .and_then(|mut session| session.take());
    let Some(mut session) = session else { return };
    for screen in &session.screens {
        if let Some(window) = app.get_webview_window(&screen.label) {
            let _ = window.destroy();
        }
    }
    if let Some(done) = session.done.take() {
        let _ = done.send(result);
    }
}

pub(crate) fn cancel(app: &AppHandle) {
    finish(app, Err(CAPTURE_CANCELLED.to_string()));
}

/// The full capture as raw bytes. A custom URI scheme loaded from the app's
/// own origin came back blank in WebKitGTK, so the page builds a Blob instead.
#[tauri::command]
pub fn overlay_image(window: WebviewWindow) -> Result<Response, String> {
    window
        .app_handle()
        .state::<SelectionState>()
        .0
        .lock()
        .map_err(|error| format!("锁定选区状态失败: {error}"))?
        .as_ref()
        .map(|session| Response::new(session.png.clone()))
        .ok_or_else(|| "选区已结束".to_string())
}

#[tauri::command]
pub fn overlay_info(window: WebviewWindow) -> Result<OverlayInfo, String> {
    let app = window.app_handle();
    let state = app.state::<SelectionState>();
    let session = state
        .0
        .lock()
        .map_err(|error| format!("锁定选区状态失败: {error}"))?;
    let session = session.as_ref().ok_or_else(|| "选区已结束".to_string())?;
    let screen = session
        .screens
        .iter()
        .find(|screen| screen.label == window.label())
        .ok_or_else(|| "未知的选区窗口".to_string())?;
    Ok(OverlayInfo {
        session: session.id.clone(),
        image_width: session.image_size.0,
        image_height: session.image_size.1,
        crop: screen.crop,
    })
}

#[tauri::command]
pub fn overlay_ready(window: WebviewWindow) -> Result<(), String> {
    reveal(window.app_handle(), &window)
}

#[tauri::command]
pub fn finish_selection(window: WebviewWindow, region: Region) -> Result<(), String> {
    let app = window.app_handle().clone();
    let screen = screen_for(&app, window.label())?;
    let crop = region_to_crop(&region, screen.crop)?;
    let png = app
        .state::<SelectionState>()
        .0
        .lock()
        .map_err(|error| format!("锁定选区状态失败: {error}"))?
        .as_ref()
        .map(|session| session.png.clone())
        .ok_or_else(|| "选区已结束".to_string())?;
    // Hide the overlays right away; decoding a multi-monitor capture takes a
    // moment and should not keep a frozen screen up.
    for label in app
        .webview_windows()
        .keys()
        .filter(|label| label.starts_with(OVERLAY_PREFIX))
    {
        if let Some(overlay) = app.get_webview_window(label) {
            let _ = overlay.hide();
        }
    }
    std::thread::spawn(move || {
        let result = crop_png(&png, crop).map(|png| Selected {
            png,
            monitor: screen.position,
        });
        finish(&app, result);
    });
    Ok(())
}

/// Move the keyboard grab to the overlay being dragged on, so Space and Esc
/// reach it even when the drag starts on another monitor.
#[tauri::command]
pub fn overlay_focus(window: WebviewWindow) -> Result<(), String> {
    let overlay = window.clone();
    window
        .run_on_main_thread(move || {
            if let Some(gdk_window) = overlay.gtk_window().ok().and_then(|gtk| gtk.window()) {
                grab_input(&gdk_window, 5);
            }
            let _ = overlay.set_focus();
        })
        .map_err(|error| error.to_string())
}

#[tauri::command]
pub fn cancel_selection(window: WebviewWindow) {
    cancel(window.app_handle());
}

fn region_to_crop(region: &Region, screen: Crop) -> Result<Crop, String> {
    if region.view_width <= 0.0 || region.view_height <= 0.0 {
        return Err("选区窗口尺寸无效".to_string());
    }
    let scale_x = f64::from(screen.width) / region.view_width;
    let scale_y = f64::from(screen.height) / region.view_height;
    let left = (region.x * scale_x)
        .floor()
        .clamp(0.0, f64::from(screen.width - 1)) as u32;
    let top = (region.y * scale_y)
        .floor()
        .clamp(0.0, f64::from(screen.height - 1)) as u32;
    let right = ((region.x + region.width) * scale_x)
        .ceil()
        .clamp(f64::from(left + 1), f64::from(screen.width)) as u32;
    let bottom = ((region.y + region.height) * scale_y)
        .ceil()
        .clamp(f64::from(top + 1), f64::from(screen.height)) as u32;
    Ok(Crop {
        x: screen.x + left,
        y: screen.y + top,
        width: right - left,
        height: bottom - top,
    })
}

fn crop_png(png: &[u8], crop: Crop) -> Result<Vec<u8>, String> {
    let image = image::load_from_memory_with_format(png, ImageFormat::Png)
        .map_err(|error| format!("无法读取截图: {error}"))?;
    let mut encoded = Cursor::new(Vec::new());
    image
        .crop_imm(crop.x, crop.y, crop.width, crop.height)
        .write_to(&mut encoded, ImageFormat::Png)
        .map_err(|error| format!("无法编码选区: {error}"))?;
    Ok(encoded.into_inner())
}

#[cfg(test)]
mod tests {
    use super::{region_to_crop, Crop, Region};

    const SCREEN: Crop = Crop {
        x: 1920,
        y: 0,
        width: 2560,
        height: 1440,
    };

    #[test]
    fn maps_css_pixels_onto_the_monitor_slice() {
        // A 1280x720 CSS overlay at 2x shows a 2560x1440 slice of the image.
        let region = Region {
            x: 100.0,
            y: 50.0,
            width: 200.5,
            height: 100.0,
            view_width: 1280.0,
            view_height: 720.0,
        };
        let crop = region_to_crop(&region, SCREEN).unwrap();
        assert_eq!(
            (crop.x, crop.y, crop.width, crop.height),
            (2120, 100, 401, 200)
        );
    }

    #[test]
    fn keeps_the_crop_inside_the_monitor() {
        let region = Region {
            x: -20.0,
            y: 700.0,
            width: 5000.0,
            height: 500.0,
            view_width: 1280.0,
            view_height: 720.0,
        };
        let crop = region_to_crop(&region, SCREEN).unwrap();
        assert_eq!(crop.x, 1920);
        assert!(crop.x + crop.width <= SCREEN.x + SCREEN.width);
        assert!(crop.y + crop.height <= SCREEN.height);
        assert!(crop.width >= 1 && crop.height >= 1);
    }
}
