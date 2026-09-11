use std::sync::Mutex;

#[cfg(target_os = "linux")]
use std::future::Future;

use tauri::{AppHandle, Emitter, Manager, State};
use tauri_plugin_global_shortcut::{GlobalShortcutExt, Shortcut};

use crate::config::AppConfig;

pub const DEFAULT_CAPTURE_SHORTCUT: &str = "Super+Shift+S";

#[derive(Default)]
pub struct ShortcutRuntime {
    updating: tauri::async_runtime::Mutex<()>,
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
pub async fn save_config(
    new_config: AppConfig,
    config: State<'_, Mutex<AppConfig>>,
    app: AppHandle,
) -> Result<(), String> {
    let runtime = app.state::<ShortcutRuntime>();
    let _updating = runtime
        .updating
        .try_lock()
        .map_err(|_| "正在保存快捷键，请等待当前操作完成后再试".to_string())?;
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
        // Keep the current listener alive while the portal asks for permission.
        // Do not time out a dialog that is still waiting for the user.
        let listener = bind_wayland_shortcut(app.clone(), new_config.capture_shortcut.clone())
            .await
            .map_err(|error| format!("注册快捷键失败，未替换原快捷键: {error}"))?;
        let mut wayland = runtime
            .wayland
            .lock()
            .map_err(|error| format!("锁定快捷键任务失败: {error}"))?;
        let mut current_config = config
            .lock()
            .map_err(|error| format!("锁定配置失败: {error}"))?;
        // Dropping the unstarted listener closes its session if saving fails.
        new_config.save()?;
        current_config.clone_from(&new_config);
        replace_wayland_listener(&app, &mut wayland, new_config.capture_shortcut, listener);
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

/// Spells a configured shortcut the way a user reads it, matching the wording the
/// settings panel uses so the tray label and the panel cannot disagree.
pub(crate) fn display_shortcut(shortcut: &str) -> String {
    shortcut
        .replace("CommandOrControl", "Ctrl")
        .replace("Super", "Win")
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

/// ashpd does not close a session when its proxy is dropped. Close it explicitly
/// after a failed bind/save or when replacing (aborting) an active listener.
#[cfg(target_os = "linux")]
struct WaylandSession(
    Option<ashpd::desktop::Session<ashpd::desktop::global_shortcuts::GlobalShortcuts>>,
);

#[cfg(target_os = "linux")]
impl Drop for WaylandSession {
    fn drop(&mut self) {
        if let Some(session) = self.0.take() {
            tauri::async_runtime::spawn(async move {
                if let Err(error) = session.close().await {
                    eprintln!("ScreenPin: failed to close shortcut session: {error}");
                }
            });
        }
    }
}

/// Waits for authorization, then returns a listener that owns the new session.
/// The caller can persist the configuration before replacing the old listener.
#[cfg(target_os = "linux")]
async fn bind_wayland_shortcut(
    app: AppHandle,
    configured_shortcut: String,
) -> Result<impl Future<Output = Result<(), String>> + Send + 'static, String> {
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
    let session = WaylandSession(Some(
        portal
            .create_session(CreateSessionOptions::default())
            .await
            .map_err(|error| format!("无法创建全局快捷键会话: {error}"))?,
    ));
    let session_ref = session.0.as_ref().expect("session was just created");
    // Session's public serialization exposes its D-Bus object path. Activated
    // signals are shared by all sessions, including the previous binding.
    let session_path = serde_json::to_value(session_ref)
        .map_err(|error| format!("无法读取快捷键会话标识: {error}"))?;
    let trigger = portal_shortcut(&configured_shortcut)?;
    let shortcut =
        NewShortcut::new("capture", "截取屏幕").preferred_trigger(Some(trigger.as_str()));
    let response = portal
        .bind_shortcuts(
            session_ref,
            &[shortcut],
            None,
            BindShortcutsOptions::default(),
        )
        .await
        .map_err(|error| format!("无法请求全局快捷键授权: {error}"))?
        .response()
        .map_err(|error| format!("全局快捷键未授权: {error}"))?;

    validate_portal_binding(&response)?;

    eprintln!("ScreenPin: Wayland global shortcut registered");
    Ok(async move {
        let _session = session;
        while let Some(event) = activated.next().await {
            if session_path == event.session_handle().as_str() && event.shortcut_id() == "capture" {
                let _ = app.emit("capture-requested", ());
            }
        }

        Err("全局快捷键门户会话已结束".to_string())
    })
}

#[cfg(target_os = "linux")]
fn validate_portal_binding(
    response: &ashpd::desktop::global_shortcuts::BindShortcuts,
) -> Result<(), String> {
    if response.shortcuts().iter().any(|shortcut| {
        shortcut.id() == "capture" && !shortcut.trigger_description().trim().is_empty()
    }) {
        Ok(())
    } else {
        Err("桌面门户没有为截图操作分配快捷键".to_string())
    }
}

#[cfg(target_os = "linux")]
fn is_wayland_session() -> bool {
    std::env::var("XDG_SESSION_TYPE").as_deref() == Ok("wayland")
}

/// Rewrites a Tauri accelerator into an XDG "shortcuts" trigger.
///
/// The two spellings disagree in ways that a blanket `to_uppercase` gets wrong:
/// the portal names the Windows key `LOGO` rather than `SUPER`, and the key
/// itself is an XKB keysym, where `a` and `A` are *different* symbols. Sending
/// `SUPER+SHIFT+A` therefore binds nothing, which is why a configured shortcut
/// looked registered but never fired.
#[cfg(target_os = "linux")]
pub(crate) fn portal_shortcut(shortcut: &str) -> Result<String, String> {
    validate_shortcut(shortcut)?;
    let mut parts = Vec::new();
    let mut tokens = shortcut
        .split('+')
        .filter(|token| !token.is_empty())
        .peekable();
    while let Some(token) = tokens.next() {
        // The last token is the key; everything before it is a modifier.
        if tokens.peek().is_none() {
            parts.push(portal_keysym(token));
        } else {
            parts.push(portal_modifier(token).to_string());
        }
    }
    Ok(parts.join("+"))
}

#[cfg(target_os = "linux")]
fn portal_modifier(token: &str) -> &'static str {
    match token.to_ascii_uppercase().as_str() {
        "SUPER" | "META" | "COMMAND" | "CMD" | "WIN" => "LOGO",
        "SHIFT" => "SHIFT",
        "ALT" | "OPTION" => "ALT",
        _ => "CTRL",
    }
}

/// Maps an accelerator key onto its XKB keysym name.
#[cfg(target_os = "linux")]
fn portal_keysym(token: &str) -> String {
    match token.to_ascii_uppercase().as_str() {
        "ENTER" | "RETURN" => "Return".to_string(),
        "SPACE" => "space".to_string(),
        "ESCAPE" | "ESC" => "Escape".to_string(),
        "TAB" => "Tab".to_string(),
        "BACKSPACE" => "BackSpace".to_string(),
        "DELETE" | "DEL" => "Delete".to_string(),
        "INSERT" => "Insert".to_string(),
        "HOME" => "Home".to_string(),
        "END" => "End".to_string(),
        "PAGEUP" => "Prior".to_string(),
        "PAGEDOWN" => "Next".to_string(),
        "ARROWUP" | "UP" => "Up".to_string(),
        "ARROWDOWN" | "DOWN" => "Down".to_string(),
        "ARROWLEFT" | "LEFT" => "Left".to_string(),
        "ARROWRIGHT" | "RIGHT" => "Right".to_string(),
        "PRINTSCREEN" | "PRINT" => "Print".to_string(),
        // Function keys keep their name; letters and digits are lowercase
        // keysyms, so `A` has to become `a` to match an unshifted key.
        upper if upper.starts_with('F') && upper[1..].parse::<u8>().is_ok() => upper.to_string(),
        _ => token.to_ascii_lowercase(),
    }
}

/// Starts registration in the background at launch, with X11 as a fallback.
#[cfg(target_os = "linux")]
fn restart_wayland_shortcut(app: &AppHandle, shortcut: String) -> Result<(), String> {
    let runtime = app.state::<ShortcutRuntime>();
    let mut wayland = runtime
        .wayland
        .lock()
        .map_err(|error| format!("锁定快捷键任务失败: {error}"))?;
    let app_handle = app.clone();
    let configured_shortcut = shortcut.clone();
    replace_wayland_listener(app, &mut wayland, shortcut, async move {
        bind_wayland_shortcut(app_handle, configured_shortcut)
            .await?
            .await
    });
    Ok(())
}

#[cfg(target_os = "linux")]
fn replace_wayland_listener(
    app: &AppHandle,
    wayland: &mut WaylandShortcutRuntime,
    shortcut: String,
    listener: impl Future<Output = Result<(), String>> + Send + 'static,
) {
    wayland.generation = wayland.generation.wrapping_add(1);
    let generation = wayland.generation;
    if let Some(previous) = wayland.task.take() {
        previous.abort();
    }
    if let Some(previous) = wayland.fallback.take() {
        let _ = app.global_shortcut().unregister(previous.as_str());
    }

    let app_handle = app.clone();
    let fallback_shortcut = shortcut;
    wayland.task = Some(tauri::async_runtime::spawn(async move {
        if let Err(error) = listener.await {
            let runtime = app_handle.state::<ShortcutRuntime>();
            let Ok(mut wayland) = runtime.wayland.lock() else {
                return;
            };
            if wayland.generation != generation {
                return;
            }
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

            wayland.fallback = Some(registered_shortcut);
        }
    }));
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

#[cfg(all(test, target_os = "linux"))]
mod tests {
    use super::validate_portal_binding;
    use ashpd::{
        desktop::global_shortcuts::BindShortcuts,
        zvariant::{serialized::Context, to_bytes, Value, LE},
    };
    use std::collections::HashMap;

    fn portal_response(
        shortcuts: &[(&str, Option<&str>)],
    ) -> Result<BindShortcuts, ashpd::zvariant::Error> {
        // Exercise the actual D-Bus variant dictionaries returned by the portal.
        let shortcuts: Vec<_> = shortcuts
            .iter()
            .map(|(id, trigger)| {
                let mut properties = HashMap::from([("description", Value::from("截取屏幕"))]);
                if let Some(trigger) = trigger {
                    properties.insert("trigger_description", Value::from(*trigger));
                }
                (*id, properties)
            })
            .collect();
        let response = HashMap::from([("shortcuts", Value::new(shortcuts))]);
        let encoded = to_bytes(Context::new_dbus(LE, 0), &response).unwrap();
        encoded.deserialize().map(|(response, _)| response)
    }

    #[test]
    fn accepts_a_capture_shortcut_assigned_by_the_portal() {
        let response = portal_response(&[("capture", Some("Super+Shift+S"))]).unwrap();
        assert!(validate_portal_binding(&response).is_ok());
    }

    #[test]
    fn rejects_a_capture_id_without_an_assigned_trigger() {
        assert!(portal_response(&[("capture", None)]).is_err());
        for trigger in [Some(""), Some("  ")] {
            let response = portal_response(&[("capture", trigger)]).unwrap();
            assert!(validate_portal_binding(&response).is_err());
        }
    }

    #[test]
    fn rejects_portal_responses_without_the_capture_action() {
        for response in [
            serde_json::from_str("{}").unwrap(),
            portal_response(&[]).unwrap(),
            portal_response(&[("other", Some("Super+Shift+S"))]).unwrap(),
        ] {
            assert!(validate_portal_binding(&response).is_err());
        }
    }
}
