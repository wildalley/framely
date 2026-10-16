//! Exercise production PNG decoding/cropping plus the capture's data-URL/JSON
//! transport across 20 persistent workers, using a synthetic dual-monitor PNG.
//! cargo run --release --manifest-path src-tauri/Cargo.toml --example capture_memory --locked
//! Add -- --without-policy for the old allocator policy (expected to fail).

#[cfg(all(target_os = "linux", target_env = "gnu"))]
#[path = "../src/capture_image.rs"]
mod capture_image;
#[cfg(all(target_os = "linux", target_env = "gnu"))]
#[path = "../src/memory.rs"]
mod memory;

#[cfg(all(target_os = "linux", target_env = "gnu"))]
mod check {
    use base64::{engine::general_purpose::STANDARD, Engine};
    use image::{DynamicImage, GenericImageView, ImageFormat, Rgba, RgbaImage};
    use std::{
        io::Cursor,
        sync::{mpsc, Arc},
        thread,
        time::Instant,
    };

    const WIDTH: u32 = 5120;
    const HEIGHT: u32 = 1440;
    const WORKERS: usize = 20;
    const ROUNDS: usize = WORKERS * 2;

    fn pss_kib() -> u64 {
        std::fs::read_to_string("/proc/self/smaps_rollup")
            .unwrap()
            .lines()
            .find_map(|line| {
                line.strip_prefix("Pss:")?
                    .split_whitespace()
                    .next()?
                    .parse()
                    .ok()
            })
            .unwrap()
    }

    fn process_capture(png: &[u8], round: usize) {
        let (width, height) = [(860, 750), (1280, 720), (2560, 1440)][round % 3];
        // This is the same decoder, cropper and encoder the real selector uses.
        let cropped = super::capture_image::crop_png(png, 100, 0, width, height).unwrap();
        let data_url = format!("data:image/png;base64,{}", STANDARD.encode(&cropped));
        let ipc = serde_json::to_vec(&serde_json::json!({
            "dataUrl": data_url, "width": width, "height": height,
        }))
        .unwrap();
        let received: serde_json::Value = serde_json::from_slice(&ipc).unwrap();
        let bytes = STANDARD
            .decode(
                received["dataUrl"]
                    .as_str()
                    .unwrap()
                    .split_once(',')
                    .unwrap()
                    .1,
            )
            .unwrap();
        let decoded = image::load_from_memory(&bytes).unwrap();
        assert_eq!(decoded.dimensions(), (width, height));
        assert_eq!(decoded.get_pixel(0, 0), Rgba([33, 188, 14, 255]));
    }

    pub fn run() {
        if !std::env::args().any(|arg| arg == "--without-policy") {
            super::memory::configure_image_allocator();
        }
        let started = Instant::now();
        let image = RgbaImage::from_fn(WIDTH, HEIGHT, |x, y| {
            Rgba([
                ((x / 3 + y * 13) % 256) as u8,
                ((x * 7 + y / 5) % 256) as u8,
                ((x / 7 + y * 3) % 256) as u8,
                255,
            ])
        });
        let mut encoded = Cursor::new(Vec::new());
        DynamicImage::ImageRgba8(image)
            .write_to(&mut encoded, ImageFormat::Png)
            .unwrap();
        let png = Arc::new(encoded.into_inner());
        let mut workers = Vec::new();
        let mut handles = Vec::new();
        for _ in 0..WORKERS {
            let (send, receive) = mpsc::channel::<(Arc<Vec<u8>>, usize, mpsc::Sender<()>)>();
            handles.push(thread::spawn(move || {
                for (png, round, done) in receive {
                    process_capture(&png, round);
                    done.send(()).unwrap();
                }
            }));
            workers.push(send);
        }
        let baseline = pss_kib();
        let mut peak_idle = baseline;
        println!(
            "baseline {:.1} MiB PSS; {} workers, {}x{} source PNG",
            baseline as f64 / 1024.0,
            WORKERS,
            WIDTH,
            HEIGHT
        );
        for round in 0..ROUNDS {
            let (send, receive) = mpsc::channel();
            workers[round % WORKERS]
                .send((png.clone(), round, send))
                .unwrap();
            receive.recv().unwrap();
            let pss = pss_kib();
            peak_idle = peak_idle.max(pss);
            if round == 0 || (round + 1) % 5 == 0 {
                println!("capture {}: {:.1} MiB PSS", round + 1, pss as f64 / 1024.0);
            }
        }
        // All image work is complete, but keep runtime workers alive just as
        // the desktop app does. Retaining a desktop frame per worker fails.
        let budget_kib = u64::from(WIDTH) * u64::from(HEIGHT) * 4 / 1024;
        let growth = peak_idle.saturating_sub(baseline);
        drop(workers);
        for handle in handles {
            handle.join().unwrap();
        }
        println!(
            "max retained growth {:.1} MiB, budget {:.1} MiB; elapsed {:.1}s",
            growth as f64 / 1024.0,
            budget_kib as f64 / 1024.0,
            started.elapsed().as_secs_f64()
        );
        assert!(
            growth <= budget_kib,
            "retained image buffers grew beyond one desktop RGBA frame"
        );
    }
}

#[cfg(all(target_os = "linux", target_env = "gnu"))]
fn main() {
    check::run();
}
#[cfg(not(all(target_os = "linux", target_env = "gnu")))]
fn main() {
    eprintln!("This regression check requires glibc on Linux.");
}
