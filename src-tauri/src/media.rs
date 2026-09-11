use std::{borrow::Cow, fs, io::Cursor, path::Path};

use arboard::{Clipboard, ImageData};
use base64::{engine::general_purpose::STANDARD, Engine as _};
use image::{DynamicImage, GenericImageView, ImageFormat, ImageReader, RgbaImage};
use serde::Serialize;

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
pub struct CaptureResult {
    data_url: String,
    width: u32,
    height: u32,
}

pub(crate) fn png_data_url(bytes: &[u8]) -> Result<CaptureResult, String> {
    // Every capture path already hands us PNG. Decoding and re-compressing a
    // full-screen image just to arrive at the same bytes is the most expensive
    // step on the capture path, so read the dimensions out of the header and
    // pass the original bytes through. Anything else still round-trips below.
    if image::guess_format(bytes).ok() == Some(ImageFormat::Png) {
        if let Ok((width, height)) =
            ImageReader::with_format(Cursor::new(bytes), ImageFormat::Png).into_dimensions()
        {
            return Ok(CaptureResult {
                data_url: format!("data:image/png;base64,{}", STANDARD.encode(bytes)),
                width,
                height,
            });
        }
    }

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

#[cfg(test)]
mod tests {
    use super::png_data_url;
    use base64::{engine::general_purpose::STANDARD, Engine as _};
    use image::{DynamicImage, ImageFormat, RgbaImage};
    use std::io::Cursor;

    fn png_bytes(width: u32, height: u32) -> Vec<u8> {
        let mut encoded = Cursor::new(Vec::new());
        DynamicImage::ImageRgba8(RgbaImage::new(width, height))
            .write_to(&mut encoded, ImageFormat::Png)
            .unwrap();
        encoded.into_inner()
    }

    #[test]
    fn passes_png_through_untouched_with_real_dimensions() {
        let bytes = png_bytes(37, 19);
        let result = png_data_url(&bytes).unwrap();
        assert_eq!((result.width, result.height), (37, 19));
        // The fast path must hand back the original bytes, not a re-encode.
        let encoded = result
            .data_url
            .strip_prefix("data:image/png;base64,")
            .unwrap();
        assert_eq!(STANDARD.decode(encoded).unwrap(), bytes);
    }

    #[test]
    fn still_converts_other_formats_to_png() {
        let mut jpeg = Cursor::new(Vec::new());
        DynamicImage::ImageRgb8(image::RgbImage::new(24, 11))
            .write_to(&mut jpeg, ImageFormat::Jpeg)
            .unwrap();
        let result = png_data_url(&jpeg.into_inner()).unwrap();
        assert_eq!((result.width, result.height), (24, 11));
        assert!(result.data_url.starts_with("data:image/png;base64,"));
    }

    #[test]
    fn rejects_data_that_is_not_an_image() {
        assert!(png_data_url(b"not an image at all").is_err());
    }
}
