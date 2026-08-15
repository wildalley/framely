use std::{fs, io, os::unix::net::UnixDatagram, path::PathBuf, thread};

use tauri::{AppHandle, Emitter};

const CAPTURE_MESSAGE: &[u8] = b"capture";

fn socket_path() -> io::Result<PathBuf> {
    dirs::runtime_dir()
        .map(|directory| directory.join("screenpin-capture.sock"))
        .ok_or_else(|| io::Error::new(io::ErrorKind::NotFound, "XDG_RUNTIME_DIR is unavailable"))
}

pub fn request_running_capture() -> io::Result<()> {
    let socket = UnixDatagram::unbound()?;
    socket.connect(socket_path()?)?;
    socket.send(CAPTURE_MESSAGE)?;
    Ok(())
}

pub fn setup_capture_listener(app: &AppHandle) -> io::Result<()> {
    let path = socket_path()?;
    let socket = match UnixDatagram::bind(&path) {
        Ok(socket) => socket,
        Err(error) if error.kind() == io::ErrorKind::AddrInUse => {
            if request_running_capture().is_ok() {
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
