use std::{fs, io, os::unix::net::UnixDatagram, path::PathBuf, thread};

use tauri::{AppHandle, Emitter};

use crate::window::show_main_window;

const CAPTURE_MESSAGE: &[u8] = b"capture";
const SHOW_MESSAGE: &[u8] = b"show";

fn socket_path() -> io::Result<PathBuf> {
    dirs::runtime_dir()
        .map(|directory| directory.join("screenpin-capture.sock"))
        .ok_or_else(|| io::Error::new(io::ErrorKind::NotFound, "XDG_RUNTIME_DIR is unavailable"))
}

fn connect_running() -> io::Result<UnixDatagram> {
    let socket = UnixDatagram::unbound()?;
    socket.connect(socket_path()?)?;
    Ok(socket)
}

pub fn request_running_capture() -> io::Result<()> {
    connect_running()?.send(CAPTURE_MESSAGE).map(|_| ())
}

/// Bring an already running instance forward instead of starting a second one.
pub fn request_running_show() -> io::Result<()> {
    connect_running()?.send(SHOW_MESSAGE).map(|_| ())
}

/// Whether another instance is listening, without asking it to do anything.
pub fn instance_running() -> bool {
    connect_running().is_ok()
}

pub fn setup_capture_listener(app: &AppHandle) -> io::Result<()> {
    let path = socket_path()?;
    let socket = match UnixDatagram::bind(&path) {
        Ok(socket) => socket,
        Err(error) if error.kind() == io::ErrorKind::AddrInUse => {
            // Only probe: a launcher that lost the race with another instance
            // must not trigger a capture in it as a side effect.
            if instance_running() {
                return Err(io::Error::new(
                    io::ErrorKind::AlreadyExists,
                    "another ScreenPin instance is already running",
                ));
            }
            fs::remove_file(&path)?;
            UnixDatagram::bind(path)?
        }
        Err(error) => return Err(error),
    };
    let app = app.clone();
    thread::Builder::new()
        .name("screenpin-capture-ipc".to_string())
        .spawn(move || {
            let mut message = [0_u8; 32];
            loop {
                match socket.recv(&mut message) {
                    Ok(size) if &message[..size] == CAPTURE_MESSAGE => {
                        let _ = app.emit("capture-requested", ());
                    }
                    Ok(size) if &message[..size] == SHOW_MESSAGE => show_main_window(&app),
                    Ok(_) => {}
                    Err(error) => {
                        eprintln!("ScreenPin: capture IPC listener stopped: {error}");
                        break;
                    }
                }
            }
        })?;
    Ok(())
}
