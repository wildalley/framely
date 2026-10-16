//! Linux desktop regression check, without portal requests or real screenshots.
//! cargo run --manifest-path src-tauri/Cargo.toml --example selection_lifecycle --locked
//! Add -- --without-cleanup to reproduce the old accumulating-process behavior.

#[cfg(target_os = "linux")]
#[path = "../src/memory.rs"]
mod memory;
#[cfg(target_os = "linux")]
#[path = "../src/overlay_window.rs"]
mod overlay_window;

#[cfg(target_os = "linux")]
mod check {
    use std::{
        collections::BTreeSet,
        fs,
        io::{Read, Write},
        net::TcpListener,
        sync::{
            atomic::{AtomicI32, Ordering},
            mpsc, Arc,
        },
        thread,
        time::{Duration, Instant},
    };

    use gtk::prelude::*;
    use tauri::{webview::PageLoadEvent, Manager, WebviewUrl, WebviewWindowBuilder};
    use webkit2gtk::WebViewExt;

    const ROUNDS: usize = 12;
    const MONITORS: usize = 2;
    const TIMEOUT: Duration = Duration::from_secs(10);

    fn sample() -> (BTreeSet<u32>, u64) {
        let pid = std::process::id();
        let mut pages = BTreeSet::new();
        let mut pss = 0;
        for path in fs::read_dir("/proc").unwrap().flatten() {
            let Ok(id) = path.file_name().to_string_lossy().parse::<u32>() else {
                continue;
            };
            let Ok(status) = fs::read_to_string(path.path().join("status")) else {
                continue;
            };
            let is_child = status.lines().any(|line| line == format!("PPid:\t{pid}"));
            if !is_child && id != pid {
                continue;
            }
            if is_child && status.starts_with("Name:\tWebKitWeb") {
                pages.insert(id);
            }
            // A terminated page process may disappear between these reads.
            let Ok(rollup) = fs::read_to_string(path.path().join("smaps_rollup")) else {
                continue;
            };
            pss += rollup
                .lines()
                .find_map(|line| {
                    line.strip_prefix("Pss:")?
                        .split_whitespace()
                        .next()?
                        .parse::<u64>()
                        .ok()
                })
                .unwrap();
        }
        (pages, pss)
    }

    fn verify_editor(app: &tauri::AppHandle, round: usize) {
        let editor = app.get_webview_window("editor").unwrap();
        let (sent, received) = mpsc::channel();
        editor.with_webview(move |view| {
            // A counter must survive every round. This detects accidentally
            // terminating/reloading the editor along with an overlay process.
            view.inner().evaluate_javascript(
                &format!("if (window.round !== {round}) throw new Error('editor state lost'); window.round += 1;"),
                None, None, None::<&gtk::gio::Cancellable>,
                move |result| { sent.send(result.is_ok()).unwrap(); },
            );
        }).unwrap();
        assert!(
            received.recv_timeout(TIMEOUT).unwrap(),
            "editor state was lost"
        );
    }

    pub fn run() {
        super::memory::configure_image_allocator();
        // Serve only a synthetic page; do not touch the editor, tray, shortcut
        // registration, configuration, IPC socket, clipboard, or capture portal.
        let server = TcpListener::bind("127.0.0.1:0").unwrap();
        let url = format!("http://{}", server.local_addr().unwrap());
        thread::spawn(move || {
            const HTML: &str = "<html><body><h1>Selection lifecycle check</h1><script>window.round = 0</script></body></html>";
            for mut stream in server.incoming().flatten() {
                let mut request = [0; 2048];
                let _ = stream.read(&mut request);
                let _ = write!(stream,
                    "HTTP/1.1 200 OK\r\nContent-Type: text/html\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{}",
                    HTML.len(), HTML);
            }
        });
        let without_cleanup = std::env::args().any(|arg| arg == "--without-cleanup");
        let result_code = Arc::new(AtomicI32::new(1));
        let worker_code = result_code.clone();
        let mut context = tauri::generate_context!();
        context.config_mut().app.windows.clear();
        tauri::Builder::default()
            .setup(move |app| {
                let (loaded, ready) = mpsc::channel();
                let editor_loaded = loaded.clone();
                WebviewWindowBuilder::new(app, "editor", WebviewUrl::External(url.parse()?))
                    .on_page_load(move |_, payload| {
                        if payload.event() == PageLoadEvent::Finished {
                            let _ = editor_loaded.send(());
                        }
                    })
                    .visible(false).build()?;
                let app = app.handle().clone();
                thread::spawn(move || {
                    let outcome = std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
                        ready.recv_timeout(TIMEOUT).unwrap();
                        let (baseline, pss) = sample();
                        assert_eq!(baseline.len(), 1, "editor must have its own process");
                        println!("baseline: {} page process, {:.1} MiB PSS", baseline.len(), pss as f64 / 1024.0);
                        for round in 1..=ROUNDS {
                            for index in 0..MONITORS {
                                let loaded = loaded.clone();
                                let window = WebviewWindowBuilder::new(&app, format!("overlay-{index}"),
                                    WebviewUrl::External(url.parse().unwrap()))
                                    .on_page_load(move |_, payload| {
                                        if payload.event() == PageLoadEvent::Finished {
                                            let _ = loaded.send(());
                                        }
                                    })
                                    .visible(false).decorations(false).skip_taskbar(true)
                                    .build().unwrap();
                                let overlay = window.clone();
                                window.run_on_main_thread(move || {
                                    // Exercise the actual overlay's X11 override-redirect
                                    // lifecycle, but keep the test outside the screen.
                                    let native = overlay.gtk_window().unwrap();
                                    native.realize();
                                    native.window().unwrap().set_override_redirect(true);
                                    native.move_(-10000, -10000);
                                    native.resize(32, 32);
                                    native.show_all();
                                }).unwrap();
                            }
                            for _ in 0..MONITORS { ready.recv_timeout(TIMEOUT).unwrap(); }
                            assert_eq!(sample().0.len(), baseline.len() + MONITORS,
                                "test must create separate overlay processes");
                            for index in 0..MONITORS {
                                let window = app.get_webview_window(&format!("overlay-{index}")).unwrap();
                                // Alternate completion and cancellation's hide-first path.
                                if round % 2 == 0 { window.hide().unwrap(); }
                                if without_cleanup { window.destroy().unwrap(); }
                                else { super::overlay_window::destroy_overlay(window); }
                            }
                            let deadline = Instant::now() + TIMEOUT;
                            let (pages, pss) = loop {
                                let current = sample();
                                if current.0 == baseline || Instant::now() >= deadline { break current; }
                                thread::sleep(Duration::from_millis(100));
                            };
                            assert_eq!(pages, baseline, "selection processes remained after round {round}");
                            assert!(app.get_webview_window("overlay-0").is_none());
                            assert!(app.get_webview_window("overlay-1").is_none());
                            verify_editor(&app, round - 1);
                            println!("round {round}: {} page process, {:.1} MiB PSS; editor state intact", pages.len(), pss as f64 / 1024.0);
                        }
                    }));
                    let code = if outcome.is_ok() { 0 } else { 1 };
                    worker_code.store(code, Ordering::SeqCst);
                    app.exit(code);
                });
                Ok(())
            })
            .build(context).unwrap()
            .run_return(|_, _| {});
        std::process::exit(result_code.load(Ordering::SeqCst));
    }
}

#[cfg(target_os = "linux")]
fn main() {
    check::run();
}

#[cfg(not(target_os = "linux"))]
fn main() {
    eprintln!("This regression check requires a Linux graphical session.");
}
