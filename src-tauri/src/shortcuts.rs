use std::sync::Mutex;

use tauri::{AppHandle, Emitter, Manager, State};
use tauri_plugin_global_shortcut::{GlobalShortcutExt, Shortcut};

use crate::config::AppConfig;

pub const DEFAULT_CAPTURE_SHORTCUT: &str = "CommandOrControl+Shift+A";

#[derive(Default)]
pub struct ShortcutRuntime {
    #[cfg(target_os = "linux")]
    wayland: Mutex<WaylandShortcutRuntime>,
}

#[cfg(target_os = "linux")]
#[derive(Default)]
struct WaylandShortcutRuntime {
    task: Option<tauri::async_runtime::JoinHandle<()>>,
    fallback: Option<String>,
    generation: u64,
}

#[tauri::command]
pub fn get_config(config: State<Mutex<AppConfig>>) -> Result<AppConfig, String> {
    config
        .lock()
        .map(|config| config.clone())
        .map_err(|error| format!("读取配置失败: {error}"))
}

#[tauri::command]
pub fn save_config(
    new_config: AppConfig,
    config: State<Mutex<AppConfig>>,
    app: AppHandle,
) -> Result<(), String> {
    validate_shortcut(&new_config.capture_shortcut)?;
    let old_shortcut = config
        .lock()
        .map_err(|error| format!("锁定配置失败: {error}"))?
        .capture_shortcut
        .clone();

    if old_shortcut == new_config.capture_shortcut {
        return new_config.save();
    }

    #[cfg(target_os = "linux")]
    if is_wayland_session() {
        new_config.save()?;
        config
            .lock()
            .map_err(|error| format!("锁定配置失败: {error}"))?
            .clone_from(&new_config);
        restart_wayland_shortcut(&app, new_config.capture_shortcut.clone())?;
        return Ok(());
    }

    // Keep the old shortcut registered until both the new registration and
    // persistence have succeeded. This makes a failed update non-destructive.
    app.global_shortcut()
        .register(new_config.capture_shortcut.as_str())
        .map_err(|error| format!("注册快捷键失败，旧快捷键仍然有效: {error}"))?;
    if let Err(error) = new_config.save() {
        let _ = app
            .global_shortcut()
            .unregister(new_config.capture_shortcut.as_str());
        return Err(error);
    }
    config
        .lock()
        .map_err(|error| format!("锁定配置失败: {error}"))?
        .clone_from(&new_config);
    let _ = app.global_shortcut().unregister(old_shortcut.as_str());
    Ok(())
}

pub(crate) fn validate_shortcut(shortcut: &str) -> Result<(), String> {
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
pub(crate) fn configure_linux_webview_backend() {
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

#[cfg(target_os = "linux")]
async fn run_wayland_shortcut(app: AppHandle, configured_shortcut: String) -> Result<(), String> {
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
    let trigger = portal_shortcut(&configured_shortcut)?;
    let shortcut =
        NewShortcut::new("capture", "截取屏幕").preferred_trigger(Some(trigger.as_str()));
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

#[cfg(target_os = "linux")]
fn is_wayland_session() -> bool {
    std::env::var("XDG_SESSION_TYPE").as_deref() == Ok("wayland")
}

#[cfg(target_os = "linux")]
pub(crate) fn portal_shortcut(shortcut: &str) -> Result<String, String> {
    validate_shortcut(shortcut)?;
    Ok(shortcut
        .replace("CommandOrControl", "CTRL")
        .replace("Control", "CTRL")
        .replace("Ctrl", "CTRL")
        .to_uppercase())
}

#[cfg(target_os = "linux")]
fn restart_wayland_shortcut(app: &AppHandle, shortcut: String) -> Result<(), String> {
    let runtime = app.state::<ShortcutRuntime>();
    let mut wayland = runtime
        .wayland
        .lock()
        .map_err(|error| format!("锁定快捷键任务失败: {error}"))?;
    wayland.generation = wayland.generation.wrapping_add(1);
    let generation = wayland.generation;
    if let Some(previous) = wayland.task.take() {
        previous.abort();
    }
    if let Some(previous) = wayland.fallback.take() {
        let _ = app.global_shortcut().unregister(previous.as_str());
    }

    let app_handle = app.clone();
    let fallback_shortcut = shortcut.clone();
    wayland.task = Some(tauri::async_runtime::spawn(async move {
        if let Err(error) = run_wayland_shortcut(app_handle.clone(), shortcut).await {
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
                Ok(mut wayland) if wayland.generation == generation => {
                    wayland.fallback = Some(registered_shortcut);
                }
                _ => {
                    let _ = app_handle
                        .global_shortcut()
                        .unregister(registered_shortcut.as_str());
                }
            };
        }
    }));
    Ok(())
}

pub(crate) fn setup_global_shortcut(app: &tauri::App) -> Result<(), Box<dyn std::error::Error>> {
    let config = app.state::<Mutex<AppConfig>>();
    let shortcut = config
        .lock()
        .map(|config| config.capture_shortcut.clone())
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
