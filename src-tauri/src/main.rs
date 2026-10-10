fn main() {
    // A second launch hands off to the running instance: `--capture` asks it to
    // capture, a plain launch brings its window forward, and an autostart
    // (`--hidden`) does nothing. Starting another copy would only fail to bind
    // the IPC socket.
    #[cfg(target_os = "linux")]
    if screenpin_lib::instance_running() {
        let arguments: Vec<_> = std::env::args_os().collect();
        let handed_off = if arguments.iter().any(|argument| argument == "--capture") {
            screenpin_lib::request_running_capture()
        } else if arguments.iter().any(|argument| argument == "--hidden") {
            Ok(())
        } else {
            screenpin_lib::request_running_show()
        };
        if handed_off.is_ok() {
            return;
        }
    }

    screenpin_lib::run();
}
