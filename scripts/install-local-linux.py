"""Install a built Linux bundle for the current user, back up and restart it."""
from pathlib import Path
import argparse
import datetime
import hashlib
import json
import os
import shutil
import signal
import subprocess
import time

repo = Path(__file__).resolve().parents[1]
parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--image', type=Path, help='Open a test image after restarting')
parser.add_argument('--replace-system-instance', action='store_true',
                    help='Also stop this user\'s ScreenPin from the system package')
args = parser.parse_args()
launch_args = ['--scroll']
if args.image is not None:
    image_path = args.image.resolve(strict=True)
    if not image_path.is_file():
        parser.error('--image must be a regular file')
    launch_args = [str(image_path)]
home = Path.home()
import platform
arch = {'x86_64': 'amd64', 'aarch64': 'arm64'}[platform.machine()]
source = repo / f'build/native/linux-{arch}/screenpin'
source_workers = source.parent / 'workers'
workers = home / '.local/share/screenpin/workers'
assert (source_workers / 'screenpin-capture').is_file()
assert (source_workers / 'screenpin-ocr/screenpin-ocr').is_file()
binary = home / '.local/bin/screenpin'
applications = home / '.local/share/applications'
desktop = applications / 'io.screenpin.desktop.desktop'
icon = home / '.local/share/icons/hicolor/128x128/apps/screenpin.png'
backup = repo / 'build/local-install-backup' / datetime.datetime.now().strftime('%Y%m%d-%H%M%S')
log = repo / 'build/native/screenpin-local.log'

version = json.loads((repo / 'mygo.json').read_text())['version']
assert subprocess.check_output([str(source), '--version'], text=True).strip() == f'ScreenPin {version} (mygo)'
backup.mkdir(parents=True)
for path, name in [(binary, 'screenpin'), (applications / 'screenpin.desktop', 'screenpin.desktop'),
                   (desktop, desktop.name), (icon, 'screenpin.png')]:
    if path.exists():
        shutil.copy2(path, backup / name)

# Stage the complete runtime before replacing a running installation.
workers.parent.mkdir(parents=True, exist_ok=True)
staged_workers = workers.with_name('.workers.installing-' + backup.name)
shutil.copytree(source_workers, staged_workers, symlinks=True)

result = subprocess.run(['pgrep', '-x', 'screenpin'], capture_output=True, text=True)
for line in result.stdout.splitlines():
    pid = int(line)
    proc = Path('/proc') / str(pid)
    try:
        executable = os.readlink(proc / 'exe').removesuffix(' (deleted)')
    except FileNotFoundError:
        continue
    allowed = {str(binary)}
    if args.replace_system_instance:
        allowed.update(['/usr/bin/screenpin', '/usr/lib/screenpin/screenpin'])
    if executable not in allowed or proc.stat().st_uid != os.getuid():
        raise RuntimeError(f'Unexpected running ScreenPin: {pid} {executable}')
    os.kill(pid, signal.SIGTERM)
    for attempt in range(100):
        if not proc.exists():
            break
        time.sleep(0.1)
    else:
        raise RuntimeError(f'Old ScreenPin did not exit: {pid}')
    print(f'Stopped old ScreenPin: {pid}')

if workers.exists():
    shutil.move(str(workers), str(backup / 'workers'))
os.replace(staged_workers, workers)

binary.parent.mkdir(parents=True, exist_ok=True)
staged = binary.with_name('.screenpin.installing')
shutil.copy2(source, staged)
staged.chmod(0o755)
os.replace(staged, binary)
icon.parent.mkdir(parents=True, exist_ok=True)
shutil.copy2(repo / 'assets/icons/128x128.png', icon)
applications.mkdir(parents=True, exist_ok=True)
entry = (repo / 'packaging/arch-native/io.screenpin.desktop.desktop').read_text()
entry = entry.replace('Exec=screenpin\n', f'Exec="{binary}"\n').replace('Icon=screenpin\n', f'Icon={icon}\n')
desktop.write_text(entry)
desktop.chmod(0o644)
legacy = applications / 'screenpin.desktop'
if legacy.exists():
    legacy.unlink()
if shutil.which('update-desktop-database'):
    subprocess.run(['update-desktop-database', str(applications)], check=True)

digest = lambda p: hashlib.sha256(p.read_bytes()).hexdigest()
assert digest(binary) == digest(source)
print(subprocess.check_output([str(binary), '--version'], text=True).strip())
with log.open('a') as output:
    output.write(f'\n--- Local launch {datetime.datetime.now().isoformat()} ---\n')
    output.flush()
    process = subprocess.Popen([str(binary), *launch_args], stdout=output, stderr=subprocess.STDOUT, start_new_session=True)
time.sleep(2)
if process.poll() is not None:
    raise RuntimeError(f'ScreenPin exited with {process.returncode}; see {log}')
print(f'Running PID: {process.pid}')
print(f'Installed: {binary}')
print(f'Components: {workers}')
print(f'Desktop entry: {desktop}')
print(f'Backup: {backup}')
print(f'Log: {log}')
