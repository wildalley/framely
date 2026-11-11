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
                    help='Also stop this user\'s Framely from the system package')
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
source = repo / f'build/native/linux-{arch}/framely'
source_workers = source.parent / 'workers'
workers = home / '.local/share/framely/workers'
legacy_workers = home / '.local/share/screenpin/workers'
assert (source_workers / 'framely-capture').is_file()
assert (source_workers / 'framely-ocr/framely-ocr').is_file()
binary = home / '.local/bin/framely'
command = binary
legacy_binary = binary.with_name('screenpin')
applications = home / '.local/share/applications'
desktop = applications / 'io.screenpin.desktop.desktop'
icon = home / '.local/share/icons/hicolor/128x128/apps/framely.png'
backup = repo / 'build/local-install-backup' / datetime.datetime.now().strftime('%Y%m%d-%H%M%S')
log = repo / 'build/native/framely-local.log'

version = json.loads((repo / 'mygo.json').read_text(encoding="utf-8"))['version']
assert subprocess.check_output([str(source), '--version'], text=True).strip() == f'Framely {version} (mygo)'
backup.mkdir(parents=True)
for path, name in [(binary, 'framely'), (legacy_binary, 'screenpin'),
                   (applications / 'screenpin.desktop', 'screenpin.desktop'),
                   (desktop, desktop.name), (icon, 'framely.png')]:
    if path.exists() or path.is_symlink():
        shutil.copy2(path, backup / name, follow_symlinks=False)

# Stage the complete runtime before replacing a running installation.
workers.parent.mkdir(parents=True, exist_ok=True)
staged_workers = workers.with_name('.workers.installing-' + backup.name)
shutil.copytree(source_workers, staged_workers, symlinks=True)

result = subprocess.run(['pgrep', '-x', 'framely|screenpin'], capture_output=True, text=True)
for line in result.stdout.splitlines():
    pid = int(line)
    proc = Path('/proc') / str(pid)
    try:
        executable = os.readlink(proc / 'exe').removesuffix(' (deleted)')
    except FileNotFoundError:
        continue
    allowed = {str(binary), str(legacy_binary), str(home / '.local/framely.app/framely'), str(home / '.local/screenpin.app/screenpin')}
    if args.replace_system_instance:
        allowed.update(['/usr/bin/framely', '/usr/bin/screenpin', '/usr/lib/framely/framely', '/usr/lib/screenpin/screenpin', '/opt/framely/framely', '/opt/screenpin/screenpin'])
    if executable not in allowed or proc.stat().st_uid != os.getuid():
        raise RuntimeError(f'Unexpected running Framely: {pid} {executable}')
    os.kill(pid, signal.SIGTERM)
    for attempt in range(100):
        if not proc.exists():
            break
        time.sleep(0.1)
    else:
        raise RuntimeError(f'Old Framely did not exit: {pid}')
    print(f'Stopped old Framely: {pid}')

if workers.exists():
    shutil.move(str(workers), str(backup / 'workers'))
if legacy_workers.exists():
    shutil.move(str(legacy_workers), str(backup / 'legacy-workers'))
os.replace(staged_workers, workers)

binary.parent.mkdir(parents=True, exist_ok=True)
staged = binary.with_name('.framely.installing')
shutil.copy2(source, staged)
staged.chmod(0o755)
os.replace(staged, binary)
# The real executable has the new name; only the legacy entry is a link.
staged_command = legacy_binary.with_name('.screenpin.installing-' + backup.name)
staged_command.symlink_to(binary.name)
os.replace(staged_command, legacy_binary)
icon.parent.mkdir(parents=True, exist_ok=True)
shutil.copy2(repo / 'assets/icons/128x128.png', icon)
applications.mkdir(parents=True, exist_ok=True)
entry = (repo / 'packaging/arch-native/io.screenpin.desktop.desktop').read_text(encoding="utf-8")
entry = entry.replace('Exec=framely\n', f'Exec="{command}"\n').replace('Icon=framely\n', f'Icon={icon}\n')
desktop.write_text(entry, encoding="utf-8")
desktop.chmod(0o644)
autostart = Path(os.environ.get('XDG_CONFIG_HOME', str(home / '.config'))) / 'autostart' / desktop.name
if autostart.is_file():
    shutil.copy2(autostart, backup / 'autostart.desktop')
    lines = autostart.read_text(encoding='utf-8').splitlines()
    autostart.write_text('\n'.join(f'Exec="{binary}" --hidden' if line.startswith('Exec=') else 'Name=Framely' if line.startswith('Name=') else line for line in lines) + '\n', encoding='utf-8')
legacy = applications / 'screenpin.desktop'
if legacy.exists():
    legacy.unlink()
if shutil.which('update-desktop-database'):
    subprocess.run(['update-desktop-database', str(applications)], check=True)

digest = lambda p: hashlib.sha256(p.read_bytes()).hexdigest()
assert digest(binary) == digest(source)
print(subprocess.check_output([str(command), '--version'], text=True).strip())
with log.open('a', encoding='utf-8') as output:
    output.write(f'\n--- Local launch {datetime.datetime.now().isoformat()} ---\n')
    output.flush()
    process = subprocess.Popen([str(command), *launch_args], stdout=output, stderr=subprocess.STDOUT, start_new_session=True)
time.sleep(2)
if process.poll() is not None:
    raise RuntimeError(f'Framely exited with {process.returncode}; see {log}')
print(f'Running PID: {process.pid}')
print(f'Installed: {binary}')
print(f'Command: {command}')
print(f'Components: {workers}')
print(f'Desktop entry: {desktop}')
print(f'Backup: {backup}')
print(f'Log: {log}')
