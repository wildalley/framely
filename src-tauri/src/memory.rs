/// Keep large, short-lived screenshot buffers out of per-thread malloc arenas.
pub(crate) fn configure_image_allocator() {
    #[cfg(all(target_os = "linux", target_env = "gnu"))]
    {
        use std::ffi::c_int;

        extern "C" {
            fn mallopt(parameter: c_int, value: c_int) -> c_int;
        }

        const M_MMAP_THRESHOLD: c_int = -3;
        const LARGE_BUFFER: c_int = 1024 * 1024;
        // glibc raises its mmap threshold dynamically after freeing large
        // buffers. A desktop RGBA frame then fits in an arena, so each runtime
        // worker can retain another ~30 MiB even after the image was dropped.
        // Fixing the threshold keeps those buffers independently mapped and
        // returns their pages on free, while small allocations keep using the
        // normal arenas. Set this before GTK/WebKit and runtime threads start.
        // https://sourceware.org/glibc/manual/latest/html_node/Memory-Allocation-Tunables.html
        // SAFETY: mallopt accepts these documented integer parameters and
        // does not invalidate live allocations or require external pointers.
        if unsafe { mallopt(M_MMAP_THRESHOLD, LARGE_BUFFER) } != 1 {
            eprintln!("ScreenPin: failed to configure large image allocations");
        }
    }
}
