use std::sync::Mutex;

use tauri::{
    menu::{CheckMenuItem, Menu, MenuItem, PredefinedMenuItem},
    tray::{MouseButton, MouseButtonState, TrayIconBuilder, TrayIconEvent},
    Emitter, Manager,
};

use crate::{
    autostart::{autostart_enabled, set_autostart_enabled, AutostartMenu},
    config::AppConfig,
    window::show_main_window,
};

pub(crate) fn setup_tray(app: &tauri::App) -> tauri::Result<()> {
    let config = app.state::<Mutex<AppConfig>>();
    let shortcut = config
        .lock()
        .map(|config| config.capture_shortcut.clone())
        .unwrap_or_else(|_| crate::shortcuts::DEFAULT_CAPTURE_SHORTCUT.to_string());

    let capture_label = format!("截图 ({})", crate::shortcuts::display_shortcut(&shortcut));
    let capture = MenuItem::with_id(app, "capture", capture_label, true, None::<&str>)?;
    let show = MenuItem::with_id(app, "show", "显示窗口", true, None::<&str>)?;
    let autostart = CheckMenuItem::with_id(
        app,
        "autostart",
        "开机自启",
        true,
        autostart_enabled(app.handle()),
        None::<&str>,
    )?;
    let separator = PredefinedMenuItem::separator(app)?;
    let quit = MenuItem::with_id(app, "quit", "退出", true, None::<&str>)?;
    let menu = Menu::with_items(app, &[&capture, &show, &autostart, &separator, &quit])?;

    // The settings panel toggles autostart too, so it needs to be able to bring
    // this checkbox back in sync.
    app.state::<AutostartMenu>().attach(autostart);

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
        } else if event.id() == "autostart" {
            // The menu item has already flipped its own checkmark; persist that
            // and put it back if the platform refused.
            let desired = !autostart_enabled(app);
            if let Err(error) = set_autostart_enabled(app, desired) {
                eprintln!("ScreenPin: failed to change autostart: {error}");
            }
            let _ = app.emit("autostart-changed", autostart_enabled(app));
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
