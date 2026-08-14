use std::{borrow::Cow, fs, io::Cursor, path::Path};

use arboard::{Clipboard, ImageData};
use base64::{engine::general_purpose::STANDARD, Engine as _};
use image::{DynamicImage, GenericImageView, ImageFormat, RgbaImage};
use serde::Serialize;

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
pub struct CaptureResult {
    data_url: String,
    width: u32,
    height: u32,
}

pub(crate) fn png_data_url(bytes: &[u8]) -> Result<CaptureResult, String> {
    let image = image::load_from_memory(bytes).map_err(|error| format!("无法读取截图: {error}"))?;
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

pub(crate) fn decode_data_url(data_url: &str) -> Result<Vec<u8>, String> {
    let (_, encoded) = data_url
        .split_once(',')
        .ok_or_else(|| "图片数据格式无效".to_string())?;
    STANDARD
        .decode(encoded)
        .map_err(|error| format!("图片数据解码失败: {error}"))
}

#[tauri::command]
pub fn read_clipboard_image() -> Result<CaptureResult, String> {
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
pub fn copy_image(data_url: String) -> Result<(), String> {
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
pub fn save_image(path: String, data_url: String) -> Result<(), String> {
    let bytes = decode_data_url(&data_url)?;
    fs::write(Path::new(&path), bytes).map_err(|error| format!("保存图片失败: {error}"))
}

#[tauri::command]
pub fn write_project(path: String, contents: String) -> Result<(), String> {
    fs::write(Path::new(&path), contents).map_err(|error| format!("保存工程失败: {error}"))
}

#[tauri::command]
pub fn read_project(path: String) -> Result<String, String> {
    fs::read_to_string(Path::new(&path)).map_err(|error| format!("读取工程失败: {error}"))
}
