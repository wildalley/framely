fn main() {
    #[cfg(target_os = "linux")]
    if std::env::args_os().any(|argument| argument == "--capture") {
        if screenpin_lib::request_running_capture().is_ok() {
            return;
        }
    }

    screenpin_lib::run();
}
