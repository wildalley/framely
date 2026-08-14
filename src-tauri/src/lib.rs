use std::{
    borrow::Cow,
    fs,
    io::Cursor,
    path::{Path, PathBuf},
    process::Command,
    sync::Mutex,
    time::{SystemTime, UNIX_EPOCH},
};

use arboard::{Clipboard, ImageData};
use base64::{engine::general_purpose::STANDARD, Engine as _};
use image::{DynamicImage, GenericImageView, ImageFormat, RgbaImage};
use serde::Serialize;
use tauri::{
    menu::{Menu, MenuItem, PredefinedMenuItem},
    tray::{MouseButton, MouseButtonState, TrayIconBuilder, TrayIconEvent},
    AppHandle, Emitter, Manager, Monitor, PhysicalPosition, PhysicalSize, State, WebviewWindow,
    WindowEvent,
};
use tauri_plugin_global_shortcut::{GlobalShortcutExt, Shortcut, ShortcutState};

mod config;
use config::AppConfig;

#[cfg(target_os = "linux")]
use raw_window_handle::{HasWindowHandle, RawWindowHandle};

const DEFAULT_CAPTURE_SHORTCUT: &str = "CommandOrControl+Shift+A";

#[derive(Default)]
struct ShortcutRuntime {
    #[cfg(target_os = "linux")]
    wayland: Mutex<WaylandShortcutRuntime>,
}

#[cfg(target_os = "linux")]
#[derive(Default)]
struct WaylandShortcutRuntime {
    task: Option<tauri::async_runtime::JoinHandle<()>>,
    fallback: Option<String>,
}

#[derive(Default)]
struct CaptureState {
    /// Geometry of the pending screenshot, so the selector uses the same
    /// coordinate space as the captured pixels.
    geometry: Mutex<Option<CaptureGeometry>>,
}

/// The desktop region a pending capture covers, in physical pixels.
#[derive(Clone, Copy)]
struct CaptureGeometry {
    origin: PhysicalPosition<i32>,
    size: PhysicalSize<u32>,
}

impl CaptureState {
    fn remember_geometry(&self, geometry: Option<CaptureGeometry>) -> Result<(), String> {
        *self
            .geometry
            .lock()
            .map_err(|error| format!("锁定截图状态失败: {error}"))? = geometry;
        Ok(())
    }

    fn take_geometry(&self) -> Option<CaptureGeometry> {
        self.geometry.lock().ok()?.take()
    }
}

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
struct CaptureResult {
    data_url: String,
    width: u32,
    height: u32,
}

/// One monitor's location inside the captured bitmap, in bitmap pixels. Lets the
/// frontend render a screen-picker and turn a click on a screen into that
/// screen's native-resolution frame. `primary` marks the system's primary
/// monitor, used as the anchor for laying the rest out.
#[derive(Serialize, Debug, PartialEq)]
#[serde(rename_all = "camelCase")]
struct ScreenBounds {
    x: u32,
    y: u32,
    width: u32,
    height: u32,
    primary: bool,
}

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
struct CaptureResponse {
    #[serde(flatten)]
    image: CaptureResult,
    selection_required: bool,
    fallback_used: bool,
    fallback_reason: Option<String>,
    /// The monitors inside the delivered bitmap, in bitmap pixels. Empty when
    /// the layout cannot be mapped onto the bitmap.
    screens: Vec<ScreenBounds>,
}

fn png_data_url(bytes: &[u8]) -> Result<CaptureResult, String> {
    let image = image::load_from_memory(bytes).map_err(|error| format!("无法读取截图: {error}"))?;
    encode_png(&image)
}

fn encode_png(image: &DynamicImage) -> Result<CaptureResult, String> {
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
async fn portal_capture(window: &WebviewWindow, interactive: bool) -> Result<Vec<u8>, String> {
    use ashpd::{desktop::screenshot::Screenshot, WindowIdentifier};

    let identifier = window
        .window_handle()
        .ok()
        .and_then(|handle| match handle.as_raw() {
            RawWindowHandle::Xlib(handle) => Some(WindowIdentifier::from_xid(handle.window)),
            RawWindowHandle::Xcb(handle) => {
                Some(WindowIdentifier::from_xid(handle.window.get().into()))
            }
            _ => None,
        });
    let request = Screenshot::request()
        .identifier(identifier)
        .interactive(interactive)
        .modal(interactive);
    let response = request
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

#[cfg(target_os = "linux")]
async fn primary_capture(window: &WebviewWindow, monitor: &Monitor) -> Result<Vec<u8>, String> {
    // The screenshot portal takes no region, so the monitor cannot be asked for
    // here. On a multi-monitor desktop it answers with all of them at once and
    // `capture_frame` cuts out the screen being captured.
    let _ = monitor;
    portal_capture(window, false).await
}

#[cfg(target_os = "linux")]
async fn system_capture(window: &WebviewWindow) -> Result<Vec<u8>, String> {
    portal_capture(window, true).await
}

/// The pixels to hand the in-app selector, plus the desktop region they cover
/// and where each monitor sits inside the bitmap.
///
/// A capture spanning several monitors is delivered whole, with every monitor's
/// bounds inside it (in bitmap pixels). The frontend overlays the boundaries so
/// the user clicks the screen they want, then edits that screen at its native
/// resolution (cropped from the bitmap, which is already full-resolution).
fn capture_frame(
    window: &WebviewWindow,
    monitor: &Monitor,
    bytes: &[u8],
) -> Result<(DynamicImage, CaptureGeometry, Vec<ScreenBounds>), String> {
    let image =
        image::load_from_memory(bytes).map_err(|error| format!("无法读取桌面门户截图: {error}"))?;
    let monitors = window
        .available_monitors()
        .map_err(|error| format!("无法读取显示器布局: {error}"))?;
    let image_size = PhysicalSize::new(image.width(), image.height());
    let origin = *monitor.position();
    let monitor_size = *monitor.size();

    if is_virtual_desktop_capture(image_size, monitor_size, monitors.len()) {
        let rects = screen_rects(&monitors);
        let (desktop_origin, desktop_size) = desktop_bounds(&rects)?;
        let primary = window
            .primary_monitor()
            .ok()
            .flatten()
            .map(|m| *m.position());
        let screens = monitor_screen_bounds(
            image_size,
            desktop_origin,
            desktop_size,
            &rects,
            primary.as_ref(),
        );
        return Ok((
            image,
            CaptureGeometry {
                origin: desktop_origin,
                size: desktop_size,
            },
            screens,
        ));
    }

    Ok((
        image,
        CaptureGeometry {
            origin,
            size: monitor_size,
        },
        vec![ScreenBounds {
            x: 0,
            y: 0,
            width: image_size.width,
            height: image_size.height,
            primary: true,
        }],
    ))
}

/// Where each monitor lands inside a bitmap of the whole virtual desktop, in
/// bitmap pixels. Empty when the bitmap is not an even scaling of the reported
/// layout, in which case no monitor rectangle can be trusted. The primary
/// monitor (matched by its reported layout position) is flagged as an anchor.
fn monitor_screen_bounds(
    image_size: PhysicalSize<u32>,
    desktop_origin: PhysicalPosition<i32>,
    desktop_size: PhysicalSize<u32>,
    screens: &[ScreenRect],
    primary_position: Option<&PhysicalPosition<i32>>,
) -> Vec<ScreenBounds> {
    if desktop_size.width == 0 || desktop_size.height == 0 {
        return Vec::new();
    }
    let scale_x = f64::from(image_size.width) / f64::from(desktop_size.width);
    let scale_y = f64::from(image_size.height) / f64::from(desktop_size.height);
    if !scales_agree(scale_x, scale_y) {
        return Vec::new();
    }
    screens
        .iter()
        .filter_map(|(position, size)| {
            let left = (f64::from(position.x.saturating_sub(desktop_origin.x)) * scale_x).round();
            let top = (f64::from(position.y.saturating_sub(desktop_origin.y)) * scale_y).round();
            let width = (f64::from(size.width) * scale_x).round();
            let height = (f64::from(size.height) * scale_y).round();
            if left < 0.0 || top < 0.0 || width < 1.0 || height < 1.0 {
                return None;
            }
            let x = left.min(f64::from(image_size.width)) as u32;
            let y = top.min(f64::from(image_size.height)) as u32;
            let width = (width.min(f64::from(image_size.width) - x as f64)) as u32;
            let height = (height.min(f64::from(image_size.height) - y as f64)) as u32;
            if width == 0 || height == 0 {
                return None;
            }
            let primary = primary_position.is_some_and(|pp| pp == position);
            Some(ScreenBounds { x, y, width, height, primary })
        })
        .collect()
}

/// A monitor reduced to plain geometry, so the layout maths can be tested
/// without a live window server.
type ScreenRect = (PhysicalPosition<i32>, PhysicalSize<u32>);

fn screen_rects(monitors: &[Monitor]) -> Vec<ScreenRect> {
    monitors
        .iter()
        .map(|monitor| (*monitor.position(), *monitor.size()))
        .collect()
}

/// Whether both axes of a bitmap are scaled by the same amount, within the
/// rounding a whole-pixel bitmap of a whole-pixel layout can introduce.
fn scales_agree(scale_x: f64, scale_y: f64) -> bool {
    (scale_x - scale_y).abs() <= 0.01 * scale_x.max(scale_y)
}

/// Bounding box of every monitor, in physical pixels.
fn desktop_bounds(screens: &[ScreenRect]) -> Result<ScreenRect, String> {
    let mut bounds: Option<(i32, i32, i32, i32)> = None;
    for (position, size) in screens {
        let right = position.x.saturating_add(size.width as i32);
        let bottom = position.y.saturating_add(size.height as i32);
        bounds = Some(match bounds {
            None => (position.x, position.y, right, bottom),
            Some((left, top, previous_right, previous_bottom)) => (
                left.min(position.x),
                top.min(position.y),
                previous_right.max(right),
                previous_bottom.max(bottom),
            ),
        });
    }
    let (left, top, right, bottom) = bounds.ok_or_else(|| "没有可用的显示器".to_string())?;
    Ok((
        PhysicalPosition::new(left, top),
        PhysicalSize::new((right - left).max(0) as u32, (bottom - top).max(0) as u32),
    ))
}

/// Whether a capture region is exactly one monitor, which is the only case a
/// fullscreen selector window can cover.
fn covers_one_monitor(screens: &[ScreenRect], geometry: CaptureGeometry) -> bool {
    screens
        .iter()
        .any(|(position, size)| *position == geometry.origin && *size == geometry.size)
}

fn is_virtual_desktop_capture(
    image_size: PhysicalSize<u32>,
    active_size: PhysicalSize<u32>,
    monitor_count: usize,
) -> bool {
    monitor_count > 1
        && (image_size.width > active_size.width || image_size.height > active_size.height)
}

#[cfg(target_os = "macos")]
async fn primary_capture(_window: &WebviewWindow, monitor: &Monitor) -> Result<Vec<u8>, String> {
    let scale = monitor.scale_factor();
    let position = monitor.position().to_logical::<i32>(scale);
    let size = monitor.size().to_logical::<u32>(scale);
    let region = format!(
        "-R{},{},{},{}",
        position.x, position.y, size.width, size.height
    );
    capture_with_screencapture(&["-x", region.as_str()]).await
}

#[cfg(target_os = "macos")]
async fn system_capture(_window: &WebviewWindow) -> Result<Vec<u8>, String> {
    capture_with_screencapture(&["-i", "-x"]).await
}

#[cfg(target_os = "macos")]
async fn capture_with_screencapture(args: &[&str]) -> Result<Vec<u8>, String> {
    let path = temporary_capture_path();
    let status = Command::new("screencapture")
        .args(args)
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
async fn primary_capture(_window: &WebviewWindow, monitor: &Monitor) -> Result<Vec<u8>, String> {
    capture_windows_monitor(monitor)
}

#[cfg(target_os = "windows")]
async fn system_capture(_window: &WebviewWindow) -> Result<Vec<u8>, String> {
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
fn capture_windows_monitor(monitor: &Monitor) -> Result<Vec<u8>, String> {
    use std::{ffi::c_void, ptr};

    type Handle = *mut c_void;
    type DeviceContext = *mut c_void;

    #[repr(C)]
    struct BitmapInfoHeader {
        size: u32,
        width: i32,
        height: i32,
        planes: u16,
        bit_count: u16,
        compression: u32,
        image_size: u32,
        x_pixels_per_meter: i32,
        y_pixels_per_meter: i32,
        colors_used: u32,
        colors_important: u32,
    }

    #[repr(C)]
    struct RgbQuad {
        blue: u8,
        green: u8,
        red: u8,
        reserved: u8,
    }

    #[repr(C)]
    struct BitmapInfo {
        header: BitmapInfoHeader,
        colors: [RgbQuad; 1],
    }

    #[link(name = "user32")]
    extern "system" {
        fn GetDC(window: Handle) -> DeviceContext;
        fn ReleaseDC(window: Handle, context: DeviceContext) -> i32;
    }

    #[link(name = "gdi32")]
    extern "system" {
        fn CreateCompatibleDC(context: DeviceContext) -> DeviceContext;
        fn CreateCompatibleBitmap(context: DeviceContext, width: i32, height: i32) -> Handle;
        fn SelectObject(context: DeviceContext, object: Handle) -> Handle;
        fn BitBlt(
            destination: DeviceContext,
            x: i32,
            y: i32,
            width: i32,
            height: i32,
            source: DeviceContext,
            source_x: i32,
            source_y: i32,
            operation: u32,
        ) -> i32;
        fn GetDIBits(
            context: DeviceContext,
            bitmap: Handle,
            start: u32,
            lines: u32,
            bits: *mut c_void,
            info: *mut BitmapInfo,
            usage: u32,
        ) -> i32;
        fn DeleteObject(object: Handle) -> i32;
        fn DeleteDC(context: DeviceContext) -> i32;
    }

    const SOURCE_COPY: u32 = 0x00cc_0020;
    const CAPTURE_LAYERED_WINDOWS: u32 = 0x4000_0000;
    const RGB_COLORS: u32 = 0;
    const RGB_BITMAP: u32 = 0;

    let position = *monitor.position();
    let size = *monitor.size();
    let width = i32::try_from(size.width).map_err(|_| "显示器宽度超出支持范围".to_string())?;
    let height = i32::try_from(size.height).map_err(|_| "显示器高度超出支持范围".to_string())?;
    let byte_len = usize::try_from(size.width)
        .ok()
        .and_then(|width| {
            usize::try_from(size.height)
                .ok()
                .map(|height| (width, height))
        })
        .and_then(|(width, height)| width.checked_mul(height))
        .and_then(|pixels| pixels.checked_mul(4))
        .ok_or_else(|| "显示器截图尺寸过大".to_string())?;
    let image_size = u32::try_from(byte_len).map_err(|_| "显示器截图尺寸过大".to_string())?;

    unsafe {
        let screen_context = GetDC(ptr::null_mut());
        if screen_context.is_null() {
            return Err("无法访问 Windows 屏幕".to_string());
        }
        let memory_context = CreateCompatibleDC(screen_context);
        if memory_context.is_null() {
            ReleaseDC(ptr::null_mut(), screen_context);
            return Err("无法创建 Windows 截图缓冲区".to_string());
        }
        let bitmap = CreateCompatibleBitmap(screen_context, width, height);
        if bitmap.is_null() {
            DeleteDC(memory_context);
            ReleaseDC(ptr::null_mut(), screen_context);
            return Err("无法创建 Windows 截图位图".to_string());
        }
        let previous = SelectObject(memory_context, bitmap);
        let copied = BitBlt(
            memory_context,
            0,
            0,
            width,
            height,
            screen_context,
            position.x,
            position.y,
            SOURCE_COPY | CAPTURE_LAYERED_WINDOWS,
        );
        SelectObject(memory_context, previous);

        let mut bgra = vec![0_u8; byte_len];
        let mut info = BitmapInfo {
            header: BitmapInfoHeader {
                size: std::mem::size_of::<BitmapInfoHeader>() as u32,
                width,
                // A negative height asks GDI for a top-down bitmap.
                height: -height,
                planes: 1,
                bit_count: 32,
                compression: RGB_BITMAP,
                image_size,
                x_pixels_per_meter: 0,
                y_pixels_per_meter: 0,
                colors_used: 0,
                colors_important: 0,
            },
            colors: [RgbQuad {
                blue: 0,
                green: 0,
                red: 0,
                reserved: 0,
            }],
        };
        let scan_lines = if copied == 0 {
            0
        } else {
            GetDIBits(
                memory_context,
                bitmap,
                0,
                size.height,
                bgra.as_mut_ptr().cast(),
                &mut info,
                RGB_COLORS,
            )
        };

        DeleteObject(bitmap);
        DeleteDC(memory_context);
        ReleaseDC(ptr::null_mut(), screen_context);

        if copied == 0 || scan_lines != height {
            return Err("Windows 屏幕像素采集失败".to_string());
        }

        for pixel in bgra.chunks_exact_mut(4) {
            pixel.swap(0, 2);
            pixel[3] = 255;
        }
        let image = RgbaImage::from_raw(size.width, size.height, bgra)
            .ok_or_else(|| "Windows 截图数据无效".to_string())?;
        let mut png = Cursor::new(Vec::new());
        DynamicImage::ImageRgba8(image)
            .write_to(&mut png, ImageFormat::Png)
            .map_err(|error| format!("无法编码 Windows 截图: {error}"))?;
        Ok(png.into_inner())
    }
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

fn temporary_capture_path() -> PathBuf {
    let nonce = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap_or_default()
        .as_nanos();
    std::env::temp_dir().join(format!("screenpin-{nonce}.png"))
}

#[tauri::command]
async fn capture_screen(
    window: WebviewWindow,
    capture: State<'_, CaptureState>,
) -> Result<CaptureResponse, String> {
    // Resolve the target monitor while the window is still mapped. Hiding it
    // first would make the pointer lookup fall back to the primary monitor.
    let monitor = capture_monitor(&window)?;
    let _ = window.hide();
    std::thread::sleep(std::time::Duration::from_millis(180));
    match primary_capture(&window, &monitor)
        .await
        .and_then(|bytes| capture_frame(&window, &monitor, &bytes))
    {
        Ok((frame, geometry, screens)) => {
            let image = encode_png(&frame)?;
            capture.remember_geometry(Some(geometry))?;
            Ok(CaptureResponse {
                image,
                selection_required: true,
                fallback_used: false,
                fallback_reason: None,
                screens,
            })
        }
        Err(primary_error) => {
            eprintln!(
                "ScreenPin: application capture unavailable: {primary_error}; using system capture"
            );
            // The system path needs no in-app selector, so drop any origin a
            // previous capture left behind.
            capture.remember_geometry(None)?;
            let result = system_capture(&window)
                .await
                .and_then(|bytes| png_data_url(&bytes))
                .map(|image| CaptureResponse {
                    image,
                    selection_required: false,
                    fallback_used: true,
                    fallback_reason: Some(primary_error),
                    screens: Vec::new(),
                });
            let _ = window.show();
            let _ = window.set_focus();
            result
        }
    }
}

#[tauri::command]
async fn capture_with_system(window: WebviewWindow) -> Result<CaptureResult, String> {
    let _ = window.hide();
    std::thread::sleep(std::time::Duration::from_millis(180));
    let result = system_capture(&window)
        .await
        .and_then(|bytes| png_data_url(&bytes));
    let _ = window.show();
    let _ = window.set_focus();
    result
}

#[tauri::command]
fn enter_capture_mode(
    window: WebviewWindow,
    capture: State<'_, CaptureState>,
) -> Result<(), String> {
    window
        .set_always_on_top(true)
        .map_err(|error| format!("无法置顶截图选区: {error}"))?;
    // Fullscreen the selector so it reliably covers a whole monitor and the
    // confirm button always stays reachable. When the capture spans several
    // monitors the frontend scales the whole desktop to fit (object-fit:
    // contain) and maps the drag back to native pixels; there is no window that
    // can physically cover a multi-screen virtual desktop on Wayland, so trying
    // to size and position the window over it would leaf it shifted and cut off.
    if let Some(geometry) = capture.take_geometry() {
        let monitors = window
            .available_monitors()
            .map_err(|error| format!("无法读取显示器布局: {error}"))?;
        let rects = screen_rects(&monitors);
        if covers_one_monitor(&rects, geometry) {
            // A region that is exactly one monitor can be fullscreened 1:1, so
            // the bitmap maps onto the window without any scaling.
            window
                .set_position(geometry.origin)
                .map_err(|error| format!("无法定位截图选区: {error}"))?;
            window
                .set_size(geometry.size)
                .map_err(|error| format!("无法调整截图选区尺寸: {error}"))?;
        }
    }
    window
        .set_fullscreen(true)
        .map_err(|error| format!("无法打开全屏截图选区: {error}"))?;
    window
        .show()
        .map_err(|error| format!("无法显示截图选区: {error}"))?;
    window
        .set_focus()
        .map_err(|error| format!("无法聚焦截图选区: {error}"))
}

#[tauri::command]
fn exit_capture_mode(window: WebviewWindow, pinned: bool) -> Result<(), String> {
    // Keep the previous editor from flashing while React swaps the selector
    // back to the document view.
    let _ = window.hide();
    let fullscreen_result = window
        .set_fullscreen(false)
        .map_err(|error| format!("无法退出全屏截图选区: {error}"));
    let pinned_result = window
        .set_always_on_top(pinned)
        .map_err(|error| format!("无法恢复窗口置顶状态: {error}"));
    let show_result = window
        .show()
        .map_err(|error| format!("无法恢复主窗口: {error}"));
    let focus_result = window
        .set_focus()
        .map_err(|error| format!("无法聚焦主窗口: {error}"));
    fullscreen_result?;
    pinned_result?;
    show_result?;
    focus_result
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
fn copy_text(text: String) -> Result<(), String> {
    let mut clipboard = Clipboard::new().map_err(|error| format!("无法访问剪贴板: {error}"))?;
    clipboard
        .set_text(text)
        .map_err(|error| format!("复制文字失败: {error}"))
}

#[tauri::command]
fn save_image(path: String, data_url: String) -> Result<(), String> {
    let bytes = decode_data_url(&data_url)?;
    fs::write(Path::new(&path), bytes).map_err(|error| format!("保存图片失败: {error}"))
}

/// Text recognized from an image, plus whether the engine was fully usable.
#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
struct OcrResult {
    text: String,
    /// Set when tesseract could not run because no trained language data is
    /// installed; the message explains what to install.
    setup_error: Option<String>,
}

/// Recognize text from an image via the Tesseract CLI, which is available on
/// all three platforms. A temporary PNG is written because the CLI is picky
/// about reading images from stdin on some platforms.
///
/// The language is chosen from what is actually installed: English and
/// Simplified Chinese are preferred when present, otherwise any installed
/// language is used so the command still works with a minimal install.
#[tauri::command]
fn ocr_image(data_url: String) -> Result<OcrResult, String> {
    let bytes = decode_data_url(&data_url)?;
    let path = temporary_capture_path();
    fs::write(&path, &bytes).map_err(|error| format!("无法写入待识别图片: {error}"))?;

    let languages = installed_languages().inspect_err(|_| {
        let _ = fs::remove_file(&path);
    })?;
    let lang_arg = if languages.is_empty() {
        let _ = fs::remove_file(&path);
        return Ok(OcrResult {
            text: String::new(),
            setup_error: Some(
                "未安装 Tesseract 语言数据。请安装 tesseract-data-eng（英文）和 \
                 tesseract-data-chi_sim（简体中文）后重试。"
                    .to_string(),
            ),
        });
    } else {
        languages.join("+")
    };

    let output = Command::new("tesseract")
        .arg(path.to_str().unwrap_or_default())
        .arg("stdout")
        .arg("-l")
        .arg(&lang_arg)
        .output()
        .map_err(|error| {
            let _ = fs::remove_file(&path);
            format!("无法启动 Tesseract: {error}")
        });
    let _ = fs::remove_file(&path);

    let output = output?;
    if !output.status.success() {
        let stderr = String::from_utf8_lossy(&output.stderr).trim().to_string();
        return Err(if stderr.is_empty() {
            "Tesseract 识别失败".to_string()
        } else {
            format!("Tesseract 识别失败: {stderr}")
        });
    }
    let text = String::from_utf8_lossy(&output.stdout).trim().to_string();
    Ok(OcrResult {
        text,
        setup_error: None,
    })
}

/// Languages for which `tesseract --list-langs` reports trained data.
fn installed_languages() -> Result<Vec<String>, String> {
    let output = Command::new("tesseract")
        .arg("--list-langs")
        .output()
        .map_err(|error| format!("无法启动 Tesseract: {error}"))?;
    let stdout = String::from_utf8_lossy(&output.stdout);
    let langs: Vec<String> = stdout
        .lines()
        .map(str::trim)
        .filter(|line| !line.is_empty() && !line.starts_with("List of"))
        .map(str::to_string)
        .collect();
    Ok(ocr_languages(&langs))
}

/// Pick the languages to pass to tesseract from the installed set: English +
/// Simplified Chinese when both are present (the common desktop install),
/// otherwise whatever is installed so a minimal setup still works.
fn ocr_languages(installed: &[String]) -> Vec<String> {
    if installed.iter().any(|l| l == "eng") && installed.iter().any(|l| l == "chi_sim") {
        return vec!["eng".to_string(), "chi_sim".to_string()];
    }
    let mut sorted = installed.to_vec();
    sorted.sort();
    sorted.dedup();
    sorted
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

/// The monitor the window itself sits on. Used for window geometry so the
/// editor never jumps to another screen on its own.
fn active_monitor(window: &WebviewWindow) -> Result<Monitor, String> {
    let monitor = window
        .current_monitor()
        .or_else(|_| window.primary_monitor())
        .map_err(|error| format!("无法读取显示器信息: {error}"))?;
    monitor.ok_or_else(|| "没有可用的显示器".to_string())
}

/// The monitor a screenshot should cover. This follows the pointer rather than
/// the window: `capture_screen` hides the window before grabbing pixels, and a
/// hidden window reports no current monitor, so window-based detection always
/// collapses onto the primary monitor.
fn capture_monitor(window: &WebviewWindow) -> Result<Monitor, String> {
    cursor_monitor(window).map_or_else(|| active_monitor(window), Ok)
}

fn cursor_monitor(window: &WebviewWindow) -> Option<Monitor> {
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
    gdk_backend_uses_x11(gdk_backend.as_deref(), session_type.as_deref())
}

#[cfg(not(target_os = "linux"))]
fn cursor_position_is_reliable() -> bool {
    true
}

/// GTK reports a constant (0, 0) pointer position on its Wayland backend, so
/// the cursor can only choose a monitor when GDK is talking X11. Wayland offers
/// clients no way to query the global pointer, so there is nothing to fall back
/// to there beyond the window's own monitor.
#[cfg(target_os = "linux")]
fn gdk_backend_uses_x11(gdk_backend: Option<&str>, session_type: Option<&str>) -> bool {
    match gdk_backend {
        // GDK_BACKEND is a comma-separated priority list; only the first entry
        // is guaranteed to be the one that gets used.
        Some(backend) => backend.split(',').next() == Some("x11"),
        None => session_type != Some("wayland"),
    }
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

#[tauri::command]
fn get_config(config: State<Mutex<AppConfig>>) -> Result<AppConfig, String> {
    config
        .lock()
        .map(|c| c.clone())
        .map_err(|e| format!("读取配置失败: {e}"))
}

#[tauri::command]
async fn save_config(
    new_config: AppConfig,
    config: State<'_, Mutex<AppConfig>>,
    app: AppHandle,
) -> Result<(), String> {
    validate_shortcut(&new_config.capture_shortcut)?;
    let old_shortcut = config
        .lock()
        .map_err(|e| format!("锁定配置失败: {e}"))?
        .capture_shortcut
        .clone();

    #[cfg(not(target_os = "linux"))]
    if old_shortcut == new_config.capture_shortcut {
        return new_config.save();
    }

    #[cfg(target_os = "linux")]
    if old_shortcut == new_config.capture_shortcut && !is_wayland_session() {
        return new_config.save();
    }

    #[cfg(target_os = "linux")]
    if is_wayland_session() {
        // Portal registration is asynchronous and may show a desktop approval
        // dialog. Do not claim the setting was saved until the compositor has
        // accepted the new binding.
        let task =
            prepare_wayland_shortcut(app.clone(), new_config.capture_shortcut.clone()).await?;
        if let Err(error) = new_config.save() {
            task.abort();
            return Err(error);
        }
        config
            .lock()
            .map_err(|e| format!("锁定配置失败: {e}"))?
            .clone_from(&new_config);
        install_wayland_shortcut(&app, task)?;
        return Ok(());
    }

    // Keep the old shortcut registered until both the new registration and
    // persistence have succeeded. This makes a failed update non-destructive.
    app.global_shortcut()
        .register(new_config.capture_shortcut.as_str())
        .map_err(|e| format!("注册快捷键失败，旧快捷键仍然有效: {e}"))?;
    if let Err(error) = new_config.save() {
        let _ = app
            .global_shortcut()
            .unregister(new_config.capture_shortcut.as_str());
        return Err(error);
    }
    config
        .lock()
        .map_err(|e| format!("锁定配置失败: {e}"))?
        .clone_from(&new_config);
    let _ = app.global_shortcut().unregister(old_shortcut.as_str());
    Ok(())
}

fn validate_shortcut(shortcut: &str) -> Result<(), String> {
    if shortcut.len() > 100 {
        return Err("快捷键格式无效或包含不受支持的按键".to_string());
    }
    let parsed = shortcut
        .parse::<Shortcut>()
        .map_err(|_| "快捷键格式无效或包含不受支持的按键".to_string())?;
    if parsed.mods.is_empty() {
        return Err("快捷键必须包含修饰键和普通按键".to_string());
    }
    Ok(())
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
    let config = app.state::<Mutex<AppConfig>>();
    let shortcut = config
        .lock()
        .map(|c| c.capture_shortcut.clone())
        .unwrap_or_else(|_| "Ctrl+Shift+A".to_string());

    let capture_label = format!("截图 ({})", shortcut.replace("CommandOrControl", "Ctrl"));
    let capture = MenuItem::with_id(app, "capture", capture_label, true, None::<&str>)?;
    let show = MenuItem::with_id(app, "show", "显示窗口", true, None::<&str>)?;
    let separator = PredefinedMenuItem::separator(app)?;
    let quit = MenuItem::with_id(app, "quit", "退出", true, None::<&str>)?;
    let menu = Menu::with_items(app, &[&capture, &show, &separator, &quit])?;

    let mut tray = TrayIconBuilder::with_id("screenpin-tray")
        .menu(&menu)
        .show_menu_on_left_click(false)
        .tooltip(format!(
            "ScreenPin v{} - 截图标注工具",
            env!("CARGO_PKG_VERSION")
        ));

    // Use icon.png for better quality on high-DPI displays
    if let Some(icon) = app.default_window_icon().cloned() {
        tray = tray.icon(icon);
    }

    tray.on_menu_event(|app, event| {
        if event.id() == "capture" {
            let _ = app.emit("capture-requested", ());
        } else if event.id() == "show" {
            show_main_window(app);
        } else if event.id() == "quit" {
            show_main_window(app);
            let _ = app.emit("quit-requested", ());
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
async fn prepare_wayland_shortcut(
    app: AppHandle,
    configured_shortcut: String,
) -> Result<tauri::async_runtime::JoinHandle<()>, String> {
    use ashpd::desktop::{
        global_shortcuts::{BindShortcutsOptions, GlobalShortcuts, NewShortcut},
        CreateSessionOptions,
    };
    use futures_util::StreamExt;

    // GNOME's global-shortcuts backend rejects a host (non-sandboxed) caller
    // whose D-Bus connection is not associated with an app id. Register this
    // process's application id once so session creation is not rejected with
    // "An app id is required". Repeated registration on the same connection
    // would fail ("already associated"), so the attempt is idempotent and
    // non-fatal: if the connection already has an app id, that is good enough.
    static HOST_APP_REGISTERED: std::sync::atomic::AtomicBool = std::sync::atomic::AtomicBool::new(false);
    if !HOST_APP_REGISTERED.swap(true, std::sync::atomic::Ordering::SeqCst) {
        // The host portal appends ".desktop" itself when looking up the desktop
        // file, so pass the id WITHOUT the suffix: it resolves to
        // `io.screenpin.desktop` (which the package installs). Passing
        // "io.screenpin.desktop" would make the portal search for
        // "io.screenpin.desktop.desktop" and fail with "App info not found".
        let app_id = ashpd::AppID::try_from("io.screenpin")
            .map_err(|error| format!("应用 ID 无效: {error}"))?;
        match ashpd::register_host_app(app_id).await {
            Ok(()) => eprintln!("ScreenPin: host app id registered"),
            Err(error) => eprintln!("ScreenPin: register_host_app failed (continuing): {error}"),
        }
    }

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
    let trigger = portal_shortcut(&configured_shortcut)?;
    // GNOME keeps a user's trigger choice by application + shortcut ID. Use a
    // trigger-specific ID so changing the setting cannot silently reuse the
    // previous shortcut assigned to the fixed `capture` ID.
    let shortcut_id = portal_shortcut_id(&trigger);
    let shortcut = NewShortcut::new(shortcut_id.as_str(), "截取屏幕")
        .preferred_trigger(Some(trigger.as_str()));
    let response = portal
        .bind_shortcuts(&session, &[shortcut], None, BindShortcutsOptions::default())
        .await
        .map_err(|error| format!("无法请求全局快捷键授权: {error}"))?
        .response()
        .map_err(|error| format!("全局快捷键未授权: {error}"))?;

    if !response
        .shortcuts()
        .iter()
        .any(|shortcut| shortcut.id() == shortcut_id)
    {
        return Err("桌面门户没有绑定截图快捷键".to_string());
    }

    eprintln!("ScreenPin: Wayland global shortcut registered");
    Ok(tauri::async_runtime::spawn(async move {
        // A portal binding only lives as long as its session.
        let _session = session;
        while let Some(event) = activated.next().await {
            if event.shortcut_id() == shortcut_id {
                let _ = app.emit("capture-requested", ());
            }
        }
        eprintln!("ScreenPin: Wayland global shortcut portal session ended");
    }))
}

#[cfg(target_os = "linux")]
fn is_wayland_session() -> bool {
    std::env::var("XDG_SESSION_TYPE").as_deref() == Ok("wayland")
}

/// Translate a configured shortcut into a freedesktop "shortcuts" trigger.
///
/// The spec only knows the modifiers CTRL, ALT, SHIFT, NUM and LOGO — there is
/// no SUPER — and the final key is an xkbcommon keysym name, which is
/// case-sensitive (`s`, `Return`, `Print`). Uppercasing the whole string
/// produced triggers such as `SUPER+S` or `ALT+PRINTSCREEN` that the portal
/// refuses to bind, which is why the Win key never worked on Wayland.
#[cfg(target_os = "linux")]
fn portal_shortcut(shortcut: &str) -> Result<String, String> {
    validate_shortcut(shortcut)?;
    let mut trigger: Vec<String> = Vec::new();
    let mut tokens = shortcut.split('+').peekable();
    while let Some(token) = tokens.next() {
        if tokens.peek().is_some() {
            trigger.push(portal_modifier(token)?);
        } else {
            trigger.push(portal_keysym(token)?);
        }
    }
    Ok(trigger.join("+"))
}

#[cfg(target_os = "linux")]
fn portal_modifier(token: &str) -> Result<String, String> {
    let name = match token.to_ascii_uppercase().as_str() {
        "COMMANDORCONTROL" | "COMMANDORCTRL" | "CMDORCONTROL" | "CMDORCTRL" | "CONTROL"
        | "CTRL" => "CTRL",
        "ALT" | "OPTION" => "ALT",
        "SHIFT" => "SHIFT",
        // The Win/Command key. `global-hotkey` spells it SUPER, the portal LOGO.
        "SUPER" | "META" | "COMMAND" | "CMD" => "LOGO",
        _ => return Err("快捷键格式无效或包含不受支持的按键".to_string()),
    };
    Ok(name.to_string())
}

/// The xkbcommon keysym name for a key spelled the way `global-hotkey` spells
/// it. Only keys that survive `validate_shortcut` can reach this function.
#[cfg(target_os = "linux")]
fn portal_keysym(token: &str) -> Result<String, String> {
    let upper = token.to_ascii_uppercase();
    let name = match upper.as_str() {
        "ENTER" | "NUMPADENTER" => "Return",
        "SPACE" => "space",
        "TAB" => "Tab",
        "BACKSPACE" => "BackSpace",
        "DELETE" => "Delete",
        "INSERT" => "Insert",
        "ESCAPE" => "Escape",
        "HOME" => "Home",
        "END" => "End",
        "PAGEUP" => "Prior",
        "PAGEDOWN" => "Next",
        "ARROWUP" => "Up",
        "ARROWDOWN" => "Down",
        "ARROWLEFT" => "Left",
        "ARROWRIGHT" => "Right",
        "PRINTSCREEN" => "Print",
        "SCROLLLOCK" => "Scroll_Lock",
        "PAUSE" => "Pause",
        "CAPSLOCK" => "Caps_Lock",
        "BACKQUOTE" => "grave",
        "BACKSLASH" => "backslash",
        "BRACKETLEFT" => "bracketleft",
        "BRACKETRIGHT" => "bracketright",
        "COMMA" => "comma",
        "PERIOD" => "period",
        "MINUS" => "minus",
        "EQUAL" => "equal",
        "QUOTE" => "apostrophe",
        "SEMICOLON" => "semicolon",
        "SLASH" => "slash",
        // Function keys keep their name; letters and digits are lower case
        // keysyms (`a`, `7`), which is also how xkbcommon names them.
        _ => {
            if upper.len() >= 2
                && upper.starts_with('F')
                && upper[1..].chars().all(|digit| digit.is_ascii_digit())
            {
                return Ok(upper);
            }
            if upper.len() == 1
                && upper
                    .chars()
                    .all(|character| character.is_ascii_alphanumeric())
            {
                return Ok(upper.to_ascii_lowercase());
            }
            return Err("快捷键格式无效或包含不受支持的按键".to_string());
        }
    };
    Ok(name.to_string())
}

#[cfg(target_os = "linux")]
fn portal_shortcut_id(trigger: &str) -> String {
    let suffix: String = trigger
        .chars()
        .map(|character| {
            if character.is_ascii_alphanumeric() {
                character.to_ascii_lowercase()
            } else {
                '-'
            }
        })
        .collect();
    format!("capture-{suffix}")
}

#[cfg(target_os = "linux")]
fn install_wayland_shortcut(
    app: &AppHandle,
    task: tauri::async_runtime::JoinHandle<()>,
) -> Result<(), String> {
    let runtime = app.state::<ShortcutRuntime>();
    let mut wayland = runtime
        .wayland
        .lock()
        .map_err(|error| format!("锁定快捷键任务失败: {error}"))?;
    if let Some(previous) = wayland.task.take() {
        previous.abort();
    }
    if let Some(previous) = wayland.fallback.take() {
        let _ = app.global_shortcut().unregister(previous.as_str());
    }
    wayland.task = Some(task);
    Ok(())
}

#[cfg(target_os = "linux")]
fn restart_wayland_shortcut(app: &AppHandle, shortcut: String) -> Result<(), String> {
    let app_handle = app.clone();
    let fallback_shortcut = shortcut.clone();
    tauri::async_runtime::spawn(async move {
        match prepare_wayland_shortcut(app_handle.clone(), shortcut).await {
            Ok(task) => {
                if let Err(error) = install_wayland_shortcut(&app_handle, task) {
                    eprintln!("ScreenPin: {error}");
                }
            }
            Err(error) => {
                eprintln!("ScreenPin: {error}; falling back to X11 shortcut registration");
                let registered_shortcut = match app_handle
                    .global_shortcut()
                    .register(fallback_shortcut.as_str())
                {
                    Ok(()) => fallback_shortcut,
                    Err(error) if fallback_shortcut != DEFAULT_CAPTURE_SHORTCUT => {
                        eprintln!("ScreenPin: fallback shortcut registration failed: {error}");
                        match app_handle
                            .global_shortcut()
                            .register(DEFAULT_CAPTURE_SHORTCUT)
                        {
                            Ok(()) => {
                                repair_shortcut_config(&app_handle);
                                DEFAULT_CAPTURE_SHORTCUT.to_string()
                            }
                            Err(default_error) => {
                                eprintln!(
                                    "ScreenPin: default shortcut registration failed: {default_error}"
                                );
                                return;
                            }
                        }
                    }
                    Err(error) => {
                        eprintln!("ScreenPin: fallback shortcut registration failed: {error}");
                        return;
                    }
                };

                let runtime = app_handle.state::<ShortcutRuntime>();
                match runtime.wayland.lock() {
                    Ok(mut wayland) => {
                        if let Some(previous) = wayland.task.take() {
                            previous.abort();
                        }
                        if let Some(previous) = wayland.fallback.replace(registered_shortcut) {
                            let _ = app_handle.global_shortcut().unregister(previous.as_str());
                        }
                    }
                    Err(error) => eprintln!("ScreenPin: failed to lock shortcut state: {error}"),
                };
            }
        }
    });
    Ok(())
}

fn setup_global_shortcut(app: &tauri::App) -> Result<(), Box<dyn std::error::Error>> {
    let config = app.state::<Mutex<AppConfig>>();
    let shortcut = config
        .lock()
        .map(|c| c.capture_shortcut.clone())
        .unwrap_or_else(|_| DEFAULT_CAPTURE_SHORTCUT.to_string());

    #[cfg(target_os = "linux")]
    if is_wayland_session() {
        restart_wayland_shortcut(app.handle(), shortcut)?;
        return Ok(());
    }

    if let Err(error) = app.global_shortcut().register(shortcut.as_str()) {
        eprintln!("ScreenPin: global shortcut registration failed: {error}");
        if shortcut != DEFAULT_CAPTURE_SHORTCUT {
            match app.global_shortcut().register(DEFAULT_CAPTURE_SHORTCUT) {
                Ok(()) => repair_shortcut_config(app.handle()),
                Err(fallback_error) => {
                    eprintln!("ScreenPin: default shortcut registration failed: {fallback_error}")
                }
            }
        }
    }
    Ok(())
}

fn repair_shortcut_config(app: &AppHandle) {
    let default_config = AppConfig::default();
    match app.state::<Mutex<AppConfig>>().lock() {
        Ok(mut config) => config.clone_from(&default_config),
        Err(error) => {
            eprintln!("ScreenPin: failed to repair in-memory shortcut configuration: {error}")
        }
    }
    if let Err(error) = default_config.save() {
        eprintln!("ScreenPin: failed to repair shortcut configuration: {error}");
    }
}

#[tauri::command]
fn quit_app(app: AppHandle) {
    app.exit(0);
}

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    #[cfg(target_os = "linux")]
    configure_linux_webview_backend();

    let mut config = AppConfig::load();
    if let Err(error) = validate_shortcut(&config.capture_shortcut) {
        eprintln!("ScreenPin: invalid saved shortcut ({error}); using the default");
        config = AppConfig::default();
        if let Err(error) = config.save() {
            eprintln!("ScreenPin: failed to repair shortcut configuration: {error}");
        }
    }

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
        .manage(Mutex::new(config))
        .manage(ShortcutRuntime::default())
        .manage(CaptureState::default())
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
            capture_with_system,
            enter_capture_mode,
            exit_capture_mode,
            read_clipboard_image,
            copy_image,
            copy_text,
            save_image,
            ocr_image,
            write_project,
            read_project,
            set_main_pinned,
            work_area,
            fit_window,
            start_drag,
            hide_window,
            quit_app,
            get_config,
            save_config
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

    #[test]
    fn picks_english_and_chinese_for_ocr_when_both_are_installed() {
        let installed = vec!["afr".to_string(), "chi_sim".to_string(), "eng".to_string()];
        assert_eq!(ocr_languages(&installed), vec!["eng".to_string(), "chi_sim".to_string()]);
    }

    #[test]
    fn falls_back_to_whatever_languages_are_installed_for_ocr() {
        let installed = vec!["osd".to_string(), "afr".to_string()];
        assert_eq!(ocr_languages(&installed), vec!["afr".to_string(), "osd".to_string()]);
        assert!(ocr_languages(&[]).is_empty());
    }

    #[test]
    fn validates_shortcuts_with_a_modifier_and_key() {
        assert!(validate_shortcut("CommandOrControl+Shift+A").is_ok());
        assert!(validate_shortcut("Alt+PrintScreen").is_ok());
        assert!(validate_shortcut("Ctrl+Frobnozzle").is_err());
        assert!(validate_shortcut("Ctrl+A+B").is_err());
        assert!(validate_shortcut("Shift").is_err());
        assert!(validate_shortcut("A").is_err());
        assert!(validate_shortcut("").is_err());
    }

    #[test]
    fn detects_a_virtual_desktop_capture() {
        let active = PhysicalSize::new(1920, 1080);
        assert!(is_virtual_desktop_capture(
            PhysicalSize::new(3840, 1080),
            active,
            2
        ));
        assert!(is_virtual_desktop_capture(
            PhysicalSize::new(1920, 2160),
            active,
            2
        ));
        assert!(!is_virtual_desktop_capture(active, active, 2));
        assert!(!is_virtual_desktop_capture(
            PhysicalSize::new(3840, 1080),
            active,
            1
        ));
    }

    /// Two 1440p monitors side by side with the left one pushed down, which is
    /// the layout that made captures look shifted downwards.
    fn staggered_screens() -> Vec<ScreenRect> {
        vec![
            (PhysicalPosition::new(0, 182), PhysicalSize::new(2560, 1440)),
            (
                PhysicalPosition::new(2560, 0),
                PhysicalSize::new(2560, 1440),
            ),
        ]
    }

    #[test]
    fn measures_the_virtual_desktop_bounding_box() {
        let (origin, size) = desktop_bounds(&staggered_screens()).unwrap();
        assert_eq!(origin, PhysicalPosition::new(0, 0));
        // Taller than either monitor, because one of them starts lower down.
        assert_eq!(size, PhysicalSize::new(5120, 1622));
        assert!(desktop_bounds(&[]).is_err());
    }

    /// With a bitmap exactly matching the reported layout, each screen maps to
    /// its own rectangle, so a click on it can be turned into that screen's
    /// native-resolution frame.
    #[test]
    fn maps_each_monitor_into_the_virtual_desktop_bitmap() {
        let screens = staggered_screens();
        let (origin, size) = desktop_bounds(&screens).unwrap();
        let image_size = PhysicalSize::new(5120, 1622);
        let primary = screens[0].0;
        let bounds = monitor_screen_bounds(image_size, origin, size, &screens, Some(&primary));
        assert_eq!(
            bounds,
            vec![
                ScreenBounds { x: 0, y: 182, width: 2560, height: 1440, primary: true },
                ScreenBounds { x: 2560, y: 0, width: 2560, height: 1440, primary: false },
            ]
        );
    }

    /// When the bitmap is scaled relative to the reported layout, the screen
    /// rectangles follow that scale so a later crop stays native.
    #[test]
    fn maps_screens_through_the_bitmap_scale() {
        let screens = staggered_screens();
        let (origin, size) = desktop_bounds(&screens).unwrap();
        let image_size = PhysicalSize::new(2560, 811);
        let bounds = monitor_screen_bounds(image_size, origin, size, &screens, None);
        assert_eq!(
            bounds,
            vec![
                ScreenBounds { x: 0, y: 91, width: 1280, height: 720, primary: false },
                ScreenBounds { x: 1280, y: 0, width: 1280, height: 720, primary: false },
            ]
        );
    }

    /// Uneven per-axis scaling means the layout cannot be trusted; do not hand
    /// the frontend rectangles it would crop incorrectly.
    #[test]
    fn refuses_to_map_screens_when_the_bitmap_is_unevenly_scaled() {
        let screens = staggered_screens();
        let (origin, size) = desktop_bounds(&screens).unwrap();
        let bounds = monitor_screen_bounds(
            PhysicalSize::new(2560, 1080),
            origin,
            size,
            &screens,
            None,
        );
        assert!(bounds.is_empty());
    }

    #[test]
    fn goes_fullscreen_only_for_a_single_monitor_region() {
        let screens = staggered_screens();
        assert!(covers_one_monitor(
            &screens,
            CaptureGeometry {
                origin: PhysicalPosition::new(0, 182),
                size: PhysicalSize::new(2560, 1440),
            }
        ));
        assert!(!covers_one_monitor(
            &screens,
            CaptureGeometry {
                origin: PhysicalPosition::new(0, 0),
                size: PhysicalSize::new(5120, 1622),
            }
        ));
        // Right size, wrong place.
        assert!(!covers_one_monitor(
            &screens,
            CaptureGeometry {
                origin: PhysicalPosition::new(0, 0),
                size: PhysicalSize::new(2560, 1440),
            }
        ));
    }

    #[cfg(target_os = "linux")]
    #[test]
    fn trusts_the_pointer_only_on_the_x11_gdk_backend() {
        // GTK reports a constant (0, 0) pointer on Wayland, which would silently
        // pin every capture to the leftmost monitor.
        assert!(gdk_backend_uses_x11(Some("x11"), Some("wayland")));
        assert!(!gdk_backend_uses_x11(Some("wayland"), Some("wayland")));
        // Only the first entry of the priority list is guaranteed to be used.
        assert!(gdk_backend_uses_x11(Some("x11,wayland"), Some("wayland")));
        assert!(!gdk_backend_uses_x11(Some("wayland,x11"), Some("wayland")));
        // Without an override, the session type decides.
        assert!(gdk_backend_uses_x11(None, Some("x11")));
        assert!(!gdk_backend_uses_x11(None, Some("wayland")));
        assert!(gdk_backend_uses_x11(None, None));
    }

    #[cfg(target_os = "linux")]
    #[test]
    fn converts_configured_shortcuts_for_the_wayland_portal() {
        // Modifiers are upper case spec names; the key is an xkb keysym, which
        // is case-sensitive.
        assert_eq!(
            portal_shortcut("CommandOrControl+Shift+S").unwrap(),
            "CTRL+SHIFT+s"
        );
        assert_eq!(portal_shortcut("Alt+PrintScreen").unwrap(), "ALT+Print");
        assert_eq!(portal_shortcut("Ctrl+Alt+F5").unwrap(), "CTRL+ALT+F5");
        assert_eq!(portal_shortcut("Alt+Enter").unwrap(), "ALT+Return");
    }

    /// The portal spells the Win/Command key LOGO and has no SUPER modifier, so
    /// a Super shortcut has to be translated rather than uppercased.
    #[cfg(target_os = "linux")]
    #[test]
    fn maps_the_win_key_to_the_portal_logo_modifier() {
        assert_eq!(portal_shortcut("Super+Shift+S").unwrap(), "LOGO+SHIFT+s");
        // `Super` is the spelling the recorder produces; the aliases exist for
        // hand-written configuration files.
        assert_eq!(portal_shortcut("Command+A").unwrap(), "LOGO+a");
        assert_eq!(portal_shortcut("Cmd+A").unwrap(), "LOGO+a");
        assert!(!portal_shortcut("Super+A").unwrap().contains("SUPER"));
    }

    #[cfg(target_os = "linux")]
    #[test]
    fn gives_each_wayland_trigger_its_own_portal_binding_id() {
        assert_eq!(portal_shortcut_id("CTRL+SHIFT+s"), "capture-ctrl-shift-s");
        assert_ne!(
            portal_shortcut_id("CTRL+SHIFT+s"),
            portal_shortcut_id("CTRL+SHIFT+a")
        );
        assert_ne!(
            portal_shortcut_id("CTRL+SHIFT+s"),
            portal_shortcut_id("LOGO+SHIFT+s")
        );
    }
}
