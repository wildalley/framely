"""Build the small, offline FFmpeg runtime and native recording worker.

Only fixed source archives are used. This LGPL build excludes GPL codecs and
network support; hardware H.264 is preferred and native MPEG-4 is the fallback.
"""
from pathlib import Path
import hashlib
import json
import os
import platform
import shutil
import shlex
import subprocess
import tarfile
import urllib.request

ROOT = Path(__file__).resolve().parents[1]
SYSTEM = platform.system()
ARCH = {"x86_64": "amd64", "AMD64": "amd64", "arm64": "arm64", "aarch64": "arm64"}[platform.machine()]
OS = {"Linux": "linux", "Windows": "windows", "Darwin": "darwin"}[SYSTEM]
DEST = ROOT / "resources" / f"{OS}-{ARCH}" / "workers"
WORK = ROOT / "build" / "media" / f"{OS}-{ARCH}"
PREFIX = WORK / "runtime"


def run(args, **kwargs):
    print(" ".join(map(str, args)), flush=True)
    subprocess.run(list(map(str, args)), check=True, **kwargs)


def tool(name, env):
    """Resolve a build tool by PATH order.

    Windows CreateProcess searches System32 before PATH for bare names, so
    "bash" would start the WSL launcher instead of MSYS2's bash. Resolve the
    full path ourselves and skip the system directory.
    """
    if SYSTEM != "Windows":
        return name
    system = Path(os.environ.get("SystemRoot", r"C:\Windows")).resolve()
    for entry in env.get("PATH", "").split(os.pathsep):
        found = entry and shutil.which(name, path=entry)
        if found and system not in Path(found).resolve().parents:
            return found
    raise RuntimeError(f"{name} not found outside {system}; install it with MSYS2 and add C:\\msys64\\usr\\bin to PATH")


# FFmpeg's pkg-config files omit the Media Foundation GUID libraries that the
# static h264_mf encoder needs (IID_ICodecAPI, IID_IMFTransform, ...).
WINDOWS_MEDIA_LIBS = ["mfplat.lib", "mfuuid.lib", "strmiids.lib", "ole32.lib"]


def build_env():
    env = os.environ.copy()
    if SYSTEM == "Windows":
        compiler = shutil.which("cl")
        if not compiler:
            raise RuntimeError("Run from an x64 MSVC developer shell")
        # MSYS2's /usr/bin/link is a Unix file utility, not the MSVC linker.
        env["PATH"] = str(Path(compiler).parent) + os.pathsep + env["PATH"]
    return env


def pkg_config_path():
    """PKG_CONFIG_PATH for MSYS2's pkgconf, which splits the list on ":",
    so a Windows "D:/..." path must be given in its /d/... form."""
    path = (PREFIX / "lib/pkgconfig").as_posix()
    if SYSTEM == "Windows" and path[1:2] == ":":
        path = "/" + path[0].lower() + path[2:]
    return path


def source(url, checksum, folder):
    archive = WORK / url.rsplit("/", 1)[1]
    if not archive.exists() or hashlib.sha256(archive.read_bytes()).hexdigest() != checksum:
        with urllib.request.urlopen(url, timeout=120) as response, archive.open("wb") as output:
            shutil.copyfileobj(response, output)
    if hashlib.sha256(archive.read_bytes()).hexdigest() != checksum:
        raise ValueError(f"Source checksum mismatch: {archive.name}")
    directory = WORK / folder
    if not directory.exists():
        with tarfile.open(archive) as stream:
            stream.extractall(WORK, filter="data")
    return directory


def main():
    WORK.mkdir(parents=True, exist_ok=True)
    DEST.mkdir(parents=True, exist_ok=True)
    ffmpeg = source("https://ffmpeg.org/releases/ffmpeg-9.0.2.tar.xz", "8c3850283eb25fa026482078a04051e0be17347b09ef81a0849bec15a96e002e", "ffmpeg-9.0.2")
    options = [f"--prefix={PREFIX.as_posix()}", "--disable-autodetect", "--disable-everything", "--disable-network", "--disable-doc", "--disable-debug", "--disable-ffplay", "--disable-ffprobe", "--enable-small", "--enable-pic", "--enable-static", "--disable-shared", "--enable-ffmpeg", "--enable-avcodec", "--enable-avformat", "--enable-avfilter", "--enable-swscale", "--enable-protocol=file,pipe", "--enable-demuxer=mov,rawvideo", "--enable-muxer=mp4,gif,image2,rawvideo", "--enable-decoder=h264,mpeg4,rawvideo", "--enable-encoder=mpeg4,gif,bmp,rawvideo", "--enable-parser=h264,mpeg4video", "--enable-filter=scale,fps,palettegen,paletteuse,format,split,trim,setpts", "--disable-avdevice", "--disable-zlib"]
    if ARCH == "amd64" and not shutil.which("nasm"):
        print("nasm unavailable: using portable C SIMD fallbacks", flush=True)
        options.append("--disable-x86asm")
    env = build_env()
    if SYSTEM == "Linux":
        options += ["--enable-vaapi", "--enable-encoder=h264_vaapi"]
    elif SYSTEM == "Darwin":
        options += ["--enable-videotoolbox", "--enable-encoder=h264_videotoolbox"]
    else:
        options += ["--toolchain=msvc", "--extra-cflags=-MT", "--enable-mediafoundation", "--enable-encoder=h264_mf", "--enable-d3d11va"]
    # NVIDIA headers are a build-only dependency and add no CUDA runtime bundle.
    if SYSTEM != "Darwin":
        nv = source("https://codeload.github.com/FFmpeg/nv-codec-headers/tar.gz/refs/tags/n13.0.19.0", "86d15d1a7c0ac73a0eafdfc57bebfeba7da8264595bf531cf4d8db1c22940116", "nv-codec-headers-n13.0.19.0")
        run([tool("make", env), f"PREFIX={PREFIX.as_posix()}", "install"], cwd=nv, env=env)
        env["PKG_CONFIG_PATH"] = pkg_config_path()
        options += ["--enable-ffnvcodec", "--enable-nvenc", "--enable-encoder=h264_nvenc"]
    configure = WORK / "configure-options.json"
    if not (PREFIX / "bin" / ("ffmpeg.exe" if SYSTEM == "Windows" else "ffmpeg")).exists() or not configure.exists() or json.loads(configure.read_text()) != options:
        run([tool("bash", env), "configure", *options], cwd=ffmpeg, env=env)
        run([tool("make", env), f"-j{min(os.cpu_count() or 2, 8)}"], cwd=ffmpeg, env=env)
        run([tool("make", env), "install"], cwd=ffmpeg, env=env)
        configure.write_text(json.dumps(options, indent=2) + "\n")
    suffix = ".exe" if SYSTEM == "Windows" else ""
    shutil.copy2(PREFIX / "bin" / ("ffmpeg" + suffix), DEST / ("screenpin-ffmpeg" + suffix))
    env["PKG_CONFIG_PATH"] = pkg_config_path()
    if SYSTEM == "Windows":
        # pkgconf from MSYS2 must resolve native Windows paths for cl/link.
        native_libs = shlex.split(subprocess.check_output([tool("pkg-config", env), "--msvc-syntax", "--static", "--libs", "libavformat", "libavcodec", "libavutil", "libswscale"], env=env, text=True)) + WINDOWS_MEDIA_LIBS
        run(["cl", "/nologo", "/O2", "/MT", "/std:c11", "/c", "/I" + str(PREFIX / "include"), "/Fo" + str(WORK / "recorder.obj"), ROOT / "workers/media/recorder.c"], env=env)
        run(["cl", "/nologo", "/std:c++20", "/EHsc", "/O2", "/MT", "/utf-8", "/DSCREENPIN_RECORD", "/Fo" + str(WORK / "record-windows.obj"), "/Fe" + str(DEST / "screenpin-record.exe"), ROOT / "workers/capture/windows.cpp", WORK / "recorder.obj", "/link", *native_libs, "windowsapp.lib", "d3d11.lib", "dxgi.lib", "user32.lib", "shell32.lib"], env=env)
    else:
        libs = shlex.split(subprocess.check_output(["pkg-config", "--static", "--cflags", "--libs", "libavformat", "libavcodec", "libavutil", "libswscale"], env=env, text=True))
        if SYSTEM == "Linux":
            pipewire = subprocess.check_output(["pkg-config", "--cflags", "--libs", "libpipewire-0.3"], text=True).split()
            run(["cc", "-O2", "-std=c11", "-DSCREENPIN_RECORD", ROOT / "workers/capture/pipewire.c", ROOT / "workers/media/recorder.c", "-o", DEST / "screenpin-record", *libs, *pipewire], env=env)
        else:
            run(["clang", "-O2", "-std=c11", "-c", ROOT / "workers/media/recorder.c", "-o", WORK / "recorder.o", "-I" + str(PREFIX / "include")], env=env)
            run(["clang", "-O2", "-fobjc-arc", "-fblocks", "-mmacosx-version-min=15.0", ROOT / "workers/media/macos.m", WORK / "recorder.o", "-o", DEST / "screenpin-record", "-framework", "Cocoa", "-framework", "ScreenCaptureKit", "-framework", "CoreMedia", "-framework", "CoreVideo", *libs], env=env)
        run(["strip", DEST / "screenpin-record"])
    licenses = DEST / "media-licenses"
    licenses.mkdir(exist_ok=True)
    shutil.copy2(ffmpeg / "COPYING.LGPLv2.1", licenses / "COPYING.FFmpeg.LGPLv2.1")
    (licenses / "BUILD.json").write_text(json.dumps({"source": "https://ffmpeg.org/releases/ffmpeg-9.0.2.tar.xz", "sha256": "8c3850283eb25fa026482078a04051e0be17347b09ef81a0849bec15a96e002e", "configure": options, "rebuild": "python scripts/build-media.py"}, indent=2) + "\n")
    if SYSTEM != "Darwin":
        shutil.copy2(nv / "include/ffnvcodec/nvEncodeAPI.h", licenses / "nvEncodeAPI.h")
    # LGPL 2.1 section 6(a): ship the complete corresponding FFmpeg source
    # plus the source of this MIT worker and its reproducible build script.
    # Users can modify and rebuild both executables without signature checks.
    shutil.copy2(WORK / "ffmpeg-9.0.2.tar.xz", licenses / "ffmpeg-9.0.2.tar.xz")
    if SYSTEM != "Darwin":
        shutil.copy2(WORK / "n13.0.19.0", licenses / "nv-codec-headers-n13.0.19.0.tar.gz")
    with tarfile.open(licenses / "screenpin-media-source.tar.gz", "w:gz") as archive:
        for name in ("LICENSE", "scripts/build-media.py", "workers/media", "workers/capture"):
            archive.add(ROOT / name, arcname=name)
    (licenses / "README.txt").write_text(
        "FFmpeg is statically linked under LGPL 2.1 or later (no GPL/nonfree codecs).\n"
        "Full corresponding sources, notices and build settings accompany this runtime.\n"
        "Extract screenpin-media-source.tar.gz to a directory, copy the FFmpeg and NVIDIA\n"
        "source archives into build/media/<os>-<arch>/ (rename NVIDIA archive to n13.0.19.0),\n"
        "and run python scripts/build-media.py. See BUILD.json for configuration.\n"
        "Native toolchain, Python 3.12+, make, bash, pkg-config, and optional nasm are required.\n"
        "Linux additionally needs PipeWire and VAAPI development libraries; macOS needs Xcode;\n"
        "Windows needs the x64 MSVC developer shell plus MSYS2 bash/make/pkgconf/nasm.\n"
        "The worker's source is MIT licensed. Modified components can replace the installed\n"
        "screenpin-record and screenpin-ffmpeg directly; reverse engineering for debugging\n"
        "modifications to these LGPL components is permitted.\n", encoding="utf-8")
    (DEST / "media-manifest.json").write_text(json.dumps({"protocol": 1, "ffmpeg": "9.0.2", "files": {name: hashlib.sha256((DEST / (name + suffix)).read_bytes()).hexdigest() for name in ("screenpin-record", "screenpin-ffmpeg")}}, indent=2) + "\n")

if __name__ == "__main__":
    main()
