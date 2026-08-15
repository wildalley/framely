use std::sync::Mutex;

use tauri::{Emitter, Manager, WindowEvent};
use tauri_plugin_global_shortcut::ShortcutState;

mod capture;
mod config;
#[cfg(target_os = "linux")]
mod ipc;
mod lifecycle;
mod media;
mod shortcuts;
mod tray;
mod window;

use capture::capture_screen;
use config::AppConfig;
use lifecycle::quit_app;
use media::{copy_image, read_clipboard_image, read_project, save_image, write_project};
use shortcuts::{get_config, save_config, ShortcutRuntime};
use window::{
    fit_window, hide_window, set_main_pinned, show_editor_window, start_drag, work_area,
    CaptureState,
};

#[cfg(target_os = "linux")]
pub use ipc::request_running_capture;

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    #[cfg(target_os = "linux")]
    shortcuts::configure_linux_webview_backend();

    let mut config = AppConfig::load();
    if let Err(error) = shortcuts::validate_shortcut(&config.capture_shortcut) {
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
            tray::setup_tray(app)?;
            #[cfg(target_os = "linux")]
            ipc::setup_capture_listener(app.handle())?;
            shortcuts::setup_global_shortcut(app)?;
            if std::env::args_os().any(|argument| argument == "--hidden") {
                if let Some(window) = app.get_webview_window("main") {
                    window.hide()?;
                }
            }
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
            hide_window,
            show_editor_window,
            quit_app,
            get_config,
            save_config
        ])
        .run(tauri::generate_context!())
        .expect("failed to run ScreenPin");
}

#[cfg(test)]
mod tests {
    use super::{config::AppConfig, media::decode_data_url, shortcuts::validate_shortcut};

    #[test]
    fn decodes_a_data_url() {
        let bytes = decode_data_url("data:image/png;base64,aGVsbG8=").unwrap();
        assert_eq!(bytes, b"hello");
    }

    #[test]
    fn validates_shortcuts_with_a_modifier_and_key() {
        assert!(validate_shortcut("Super+Shift+S").is_ok());
        assert!(validate_shortcut("CommandOrControl+Shift+A").is_ok());
        assert!(validate_shortcut("Alt+PrintScreen").is_ok());
        assert!(validate_shortcut("Ctrl+Frobnozzle").is_err());
        assert!(validate_shortcut("Ctrl+A+B").is_err());
        assert!(validate_shortcut("Shift").is_err());
        assert!(validate_shortcut("A").is_err());
        assert!(validate_shortcut("").is_err());
    }

    #[test]
    fn uses_win_shift_s_as_the_default_capture_shortcut() {
        assert_eq!(AppConfig::default().capture_shortcut, "Super+Shift+S");
    }

    #[cfg(target_os = "linux")]
    #[test]
    fn converts_configured_shortcuts_for_the_wayland_portal() {
        assert_eq!(
            super::shortcuts::portal_shortcut("CommandOrControl+Shift+S").unwrap(),
            "CTRL+SHIFT+S"
        );
        assert_eq!(
            super::shortcuts::portal_shortcut("Alt+PrintScreen").unwrap(),
            "ALT+PRINTSCREEN"
        );
    }
}
