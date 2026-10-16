use std::io::Cursor;

use image::{GenericImageView, ImageFormat};

pub(crate) fn crop_png(
    png: &[u8],
    x: u32,
    y: u32,
    width: u32,
    height: u32,
) -> Result<Vec<u8>, String> {
    let image = image::load_from_memory_with_format(png, ImageFormat::Png)
        .map_err(|error| format!("无法读取截图: {error}"))?;
    let (image_width, image_height) = image.dimensions();
    if width == 0
        || height == 0
        || x.checked_add(width).is_none_or(|right| right > image_width)
        || y.checked_add(height)
            .is_none_or(|bottom| bottom > image_height)
    {
        return Err("截图选区超出图片范围".to_string());
    }
    let mut encoded = Cursor::new(Vec::new());
    image
        .crop_imm(x, y, width, height)
        .write_to(&mut encoded, ImageFormat::Png)
        .map_err(|error| format!("无法编码选区: {error}"))?;
    Ok(encoded.into_inner())
}

#[cfg(test)]
mod tests {
    use super::*;
    use image::{DynamicImage, Rgba, RgbaImage};

    #[test]
    fn cropped_png_preserves_pixels_and_dimensions() {
        let pixels = RgbaImage::from_fn(8, 5, |x, y| Rgba([x as u8, y as u8, 42, 255]));
        let mut png = Cursor::new(Vec::new());
        DynamicImage::ImageRgba8(pixels.clone())
            .write_to(&mut png, ImageFormat::Png)
            .unwrap();
        let result = crop_png(png.get_ref(), 2, 1, 4, 3).unwrap();
        let cropped = image::load_from_memory(&result).unwrap().into_rgba8();
        assert_eq!(cropped.dimensions(), (4, 3));
        for (x, y, pixel) in cropped.enumerate_pixels() {
            assert_eq!(pixel, pixels.get_pixel(x + 2, y + 1));
        }
        assert!(crop_png(png.get_ref(), 7, 0, 2, 1).is_err());
        assert!(crop_png(png.get_ref(), u32::MAX, 0, 2, 1).is_err());
        assert!(crop_png(png.get_ref(), 0, 0, 0, 1).is_err());
    }

    #[test]
    fn rejects_invalid_capture() {
        assert!(crop_png(b"broken PNG", 0, 0, 1, 1).is_err());
    }
}
