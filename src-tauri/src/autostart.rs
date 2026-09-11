//! Launch-at-login support.
//!
//! The operating system is the single source of truth: the desktop entry, login
//! item or registry value written by `tauri-plugin-autostart` decides whether
//! ScreenPin starts with the session. Nothing is mirrored into `config.json`,
//! so removing the entry by hand cannot leave the UI disagreeing with reality.

use std::sync::Mutex;

use tauri::{menu::CheckMenuItem, AppHandle, Manager, Wry};
use tauri_plugin_autostart::ManagerExt;

/// Holds the tray's checkbox so the settings panel and the tray menu cannot
/// drift apart. Optional because the tray is built during setup.
#[derive(Default)]
pub struct AutostartMenu(Mutex<Option<CheckMenuItem<Wry>>>);

impl AutostartMenu {
    pub(crate) fn attach(&self, item: CheckMenuItem<Wry>) {
        if let Ok(mut slot) = self.0.lock() {
            *slot = Some(item);
        }
    }

    /// Best effort: a poisoned lock or a missing tray must not turn a
    /// successful toggle into a failure.
    fn show(&self, enabled: bool) {
        if let Ok(slot) = self.0.lock() {
            if let Some(item) = slot.as_ref() {
                let _ = item.set_checked(enabled);
            }
        }
    }
}

/// Reads the real state, falling back to `false` when the platform cannot say.
/// Used where there is no way to report an error, such as menu construction.
pub(crate) fn autostart_enabled(app: &AppHandle) -> bool {
    read_autostart(app).unwrap_or(false)
}

pub(crate) fn read_autostart(app: &AppHandle) -> Result<bool, String> {
    app.autolaunch()
        .is_enabled()
        .map_err(|error| format!("读取开机自启状态失败: {error}"))
}

pub(crate) fn set_autostart_enabled(app: &AppHandle, enabled: bool) -> Result<(), String> {
    let manager = app.autolaunch();
    let result = if enabled {
        manager
            .enable()
            .map_err(|error| format!("开启开机自启失败: {error}"))
    } else {
        manager
            .disable()
            .map_err(|error| format!("关闭开机自启失败: {error}"))
    };
    // Whether or not the change landed, leave the checkbox showing what the
    // system actually has, so a refused toggle visibly snaps back.
    app.state::<AutostartMenu>().show(autostart_enabled(app));
    result
}

#[tauri::command]
pub fn get_autostart(app: AppHandle) -> Result<bool, String> {
    read_autostart(&app)
}

#[tauri::command]
pub fn set_autostart(app: AppHandle, enabled: bool) -> Result<(), String> {
    set_autostart_enabled(&app, enabled)
}
