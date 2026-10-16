use tauri::WebviewWindow;
use webkit2gtk::WebViewExt;

/// Dispose of a short-lived, standalone selection view and its page process.
pub(crate) fn destroy_overlay(window: WebviewWindow) {
    let overlay = window.clone();
    if let Err(error) = window.with_webview(move |platform| {
        // On WebKitGTK, destroying the window/view can leave its web process
        // alive for the lifetime of the shared WebContext. Every capture then
        // adds one process per monitor. These standalone overlays have their
        // own page processes; terminate them on the GTK thread before tearing
        // down the window. Never apply this to the persistent editor view.
        platform.inner().terminate_web_process();
        if let Err(error) = overlay.destroy() {
            eprintln!("ScreenPin: failed to destroy selection window: {error}");
        }
    }) {
        eprintln!("ScreenPin: failed to release selection web process: {error}");
        // Even if dispatch to the native view failed, still remove the window.
        if let Err(error) = window.destroy() {
            eprintln!("ScreenPin: failed to destroy selection window: {error}");
        }
    }
}
