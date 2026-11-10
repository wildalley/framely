"""Exercise the native encoder using the exact libraries shipped in the bundle."""
from pathlib import Path
import importlib.util
import json
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location("build_media", ROOT / "scripts/build-media.py")
media = importlib.util.module_from_spec(spec)
spec.loader.exec_module(media)
env = media.build_env()
env["PKG_CONFIG_PATH"] = (media.PREFIX / "lib/pkgconfig").as_posix()
binary = media.WORK / ("recorder-test.exe" if media.SYSTEM == "Windows" else "recorder-test")
if media.SYSTEM == "Windows":
    import shlex
    libs = shlex.split(subprocess.check_output([media.tool("pkg-config", env), "--msvc-syntax", "--static", "--libs", "libavformat", "libavcodec", "libavutil", "libswscale"], env=env, text=True))
    media.run(["cl", "/nologo", "/O2", "/MT", "/std:c11", "/I"+str(media.PREFIX / "include"), "/Fo"+str(media.WORK)+"\\", "/Fe"+str(binary), ROOT / "workers/media/recorder_test.c", ROOT / "workers/media/recorder.c", "/link", *libs])
else:
    import shlex
    libs = shlex.split(subprocess.check_output(["pkg-config", "--static", "--cflags", "--libs", "libavformat", "libavcodec", "libavutil", "libswscale"], env=env, text=True))
    media.run(["cc", "-O2", "-Wall", "-Wextra", "-Werror", "-std=c11", ROOT / "workers/media/recorder_test.c", ROOT / "workers/media/recorder.c", "-o", binary, *libs])
clip = media.WORK / "timing-test.mp4"
status = json.loads(subprocess.check_output([str(binary), str(clip), "mpeg4"], text=True))
assert status["finished"] and status["frames"] == 120 and status["elapsedMs"] == 2000 and status["missed"] == 0, status
crop = json.loads(subprocess.check_output([str(binary), str(media.WORK / "static-crop.mp4"), "mpeg4", "crop"], text=True))
assert crop["frames"] == 60 and crop["repeated"] == 59 and crop["width"] == 64 and crop["height"] == 64 and crop["elapsedMs"] == 1000, crop
subprocess.run([str(binary), str(media.WORK / "nonexistent-directory/video.mp4"), "mpeg4", "failure"], check=True)
if media.SYSTEM == "Linux":
    subprocess.run([str(binary), "/dev/full", "mpeg4", "failure"], check=True)
ffmpeg = media.DEST / ("screenpin-ffmpeg.exe" if media.SYSTEM == "Windows" else "screenpin-ffmpeg")
# Decode into a small stream and count frames without retaining an entire clip.
cmd = [str(ffmpeg), "-hide_banner", "-loglevel", "error", "-i", str(clip), "-vf", "scale=16:16", "-pix_fmt", "rgba", "-f", "rawvideo", "pipe:1"]
process = subprocess.Popen(cmd, stdout=subprocess.PIPE)
count = 0
while data := process.stdout.read(65536):
    count += len(data)
process.stdout.close()
assert process.wait(timeout=10) == 0
assert count == 120*16*16*4, count
print("Native 1080p60: decoded 120 frames; a 5-second pause excluded from the 2-second clip")
env.update(SCREENPIN_COMPONENTS_DIR=str(media.DEST), SCREENPIN_MEDIA_INTEGRATION="1")
subprocess.run(["go", "test", "-count=1", "-run", "TestPackagedMediaExports", "-v", "./internal/recording"], cwd=ROOT, env=env, check=True)
