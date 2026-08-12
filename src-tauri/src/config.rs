use serde::{Deserialize, Serialize};
use std::fs;
use std::path::PathBuf;

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct AppConfig {
    #[serde(default = "default_capture_shortcut")]
    pub capture_shortcut: String,
}

fn default_capture_shortcut() -> String {
    "CommandOrControl+Shift+A".to_string()
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
        let contents = serde_json::to_string_pretty(self)
            .map_err(|e| format!("序列化配置失败: {e}"))?;
        fs::write(&path, contents).map_err(|e| format!("保存配置失败: {e}"))
    }
}

fn config_path() -> Option<PathBuf> {
    dirs::config_dir().map(|dir| dir.join("screenpin").join("config.json"))
}
