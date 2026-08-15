use serde::{Deserialize, Serialize};
use std::fs;
use std::path::PathBuf;

#[cfg(target_os = "windows")]
use std::{ffi::OsStr, os::windows::ffi::OsStrExt, path::Path};

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct AppConfig {
    #[serde(default = "default_capture_shortcut")]
    pub capture_shortcut: String,
}

fn default_capture_shortcut() -> String {
    "Super+Shift+S".to_string()
}

impl Default for AppConfig {
    fn default() -> Self {
        Self {
            capture_shortcut: default_capture_shortcut(),
        }
    }
}

impl AppConfig {
    pub fn load() -> Self {
        if let Some(path) = config_path() {
            if let Ok(contents) = fs::read_to_string(&path) {
                if let Ok(config) = serde_json::from_str(&contents) {
                    return config;
                }
            }
        }
        Self::default()
    }

    pub fn save(&self) -> Result<(), String> {
        let path = config_path().ok_or_else(|| "无法确定配置文件路径".to_string())?;
        if let Some(parent) = path.parent() {
            fs::create_dir_all(parent).map_err(|e| format!("创建配置目录失败: {e}"))?;
        }
        let contents =
            serde_json::to_string_pretty(self).map_err(|e| format!("序列化配置失败: {e}"))?;
        let temporary = path.with_extension("json.tmp");
        fs::write(&temporary, contents).map_err(|e| format!("保存临时配置失败: {e}"))?;
        replace_file(&temporary, &path).map_err(|e| {
            let _ = fs::remove_file(&temporary);
            format!("替换配置文件失败: {e}")
        })
    }
}

#[cfg(not(target_os = "windows"))]
fn replace_file(source: &std::path::Path, destination: &std::path::Path) -> std::io::Result<()> {
    fs::rename(source, destination)
}

#[cfg(target_os = "windows")]
fn replace_file(source: &Path, destination: &Path) -> std::io::Result<()> {
    #[link(name = "kernel32")]
    extern "system" {
        fn MoveFileExW(
            existing_file_name: *const u16,
            new_file_name: *const u16,
            flags: u32,
        ) -> i32;
    }

    const MOVEFILE_REPLACE_EXISTING: u32 = 0x1;
    const MOVEFILE_WRITE_THROUGH: u32 = 0x8;

    fn wide_path(path: &OsStr) -> Vec<u16> {
        path.encode_wide().chain(std::iter::once(0)).collect()
    }

    let source = wide_path(source.as_os_str());
    let destination = wide_path(destination.as_os_str());
    let moved = unsafe {
        MoveFileExW(
            source.as_ptr(),
            destination.as_ptr(),
            MOVEFILE_REPLACE_EXISTING | MOVEFILE_WRITE_THROUGH,
        )
    };
    if moved == 0 {
        Err(std::io::Error::last_os_error())
    } else {
        Ok(())
    }
}

fn config_path() -> Option<PathBuf> {
    dirs::config_dir().map(|dir| dir.join("screenpin").join("config.json"))
}
