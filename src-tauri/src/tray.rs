use std::sync::Mutex;

use tauri::{
    menu::{Menu, MenuItem, PredefinedMenuItem},
    tray::{MouseButton, MouseButtonState, TrayIconBuilder, TrayIconEvent},
    Emitter, Manager,
};

use crate::{config::AppConfig, window::show_main_window};

pub(crate) fn setup_tray(app: &tauri::App) -> tauri::Result<()> {
    let config = app.state::<Mutex<AppConfig>>();
    let shortcut = config
        .lock()
        .map(|config| config.capture_shortcut.clone())
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
        .tooltip("ScreenPin - 截图标注工具");

    // Use icon.png for better quality on high-DPI displays.
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
