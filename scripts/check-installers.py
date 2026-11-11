"""Exercise actual SDK bundles without rebuilding OCR or FFmpeg."""
import importlib.util
import json
from pathlib import Path
import platform
import plistlib
import shutil
import subprocess
import tarfile
import tempfile

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('package_release', ROOT / 'scripts/package-release.py')
package = importlib.util.module_from_spec(spec)
spec.loader.exec_module(package)
version = json.loads((ROOT / 'mygo.json').read_text(encoding='utf-8'))['version']
arch = {'x86_64': 'amd64', 'AMD64': 'amd64', 'arm64': 'arm64', 'aarch64': 'arm64'}[platform.machine()]
system = platform.system()


def linux():
    bundle = ROOT / f'build/native/linux-{arch}'
    with tempfile.TemporaryDirectory(prefix='framely installer ') as directory:
        tmp = Path(directory)
        out = tmp / 'assets'
        out.mkdir()
        # Small worker fixtures exercise placement. Full release packaging
        # separately verifies and executes the real capture/OCR/media workers.
        workers = tmp / 'workers'
        workers.mkdir()
        (workers / 'framely-capture').touch()
        package.package_linux(bundle, workers, out, version, arch)
        deb = out / f'framely-{version}-linux-{arch}.deb'
        fields = subprocess.check_output(['dpkg-deb', '--field', str(deb)], text=True)
        for field in ('Package: framely', 'Provides: screenpin', 'Conflicts: screenpin', 'Replaces: screenpin'):
            assert field in fields, fields
        unpacked = tmp / 'deb'
        subprocess.run(['dpkg-deb', '--extract', str(deb), str(unpacked)], check=True)
        assert (unpacked / 'opt/framely/framely').is_file()
        assert (unpacked / 'opt/screenpin/screenpin').is_symlink()
        assert (unpacked / 'usr/share/applications' / package.DESKTOP_ID).is_file()
        assert not (unpacked / 'usr/share/applications/screenpin.desktop').exists()
        assert subprocess.check_output([str(unpacked / 'opt/framely/framely'), '--version'], text=True).strip() == f'Framely {version} (mygo)'
        source = tmp / 'portable'
        with tarfile.open(out / f'framely-{version}-linux-{arch}-portable.tar.gz') as archive:
            archive.extractall(source, filter='data')
        original = (source / 'install.sh').read_text(encoding='utf-8')
        for previous in ('fresh', 'portable', 'local', 'unrelated'):
            home = tmp / f'user {previous}'
            local = home / '.local'
            commands = local / 'bin'
            commands.mkdir(parents=True)
            desktop = local / 'share/applications' / package.DESKTOP_ID
            desktop.parent.mkdir(parents=True)
            autostart = home / '.config/autostart' / package.DESKTOP_ID
            autostart.parent.mkdir(parents=True)
            autostart.write_text('[Desktop Entry]\nName=ScreenPin\nExec=old --hidden\nHidden=true\nX-GNOME-Autostart-enabled=false\n', encoding='utf-8')
            if previous == 'portable':
                old = local / 'screenpin.app'
                old.mkdir()
                (old / 'screenpin').write_text('old executable', encoding='utf-8')
                (old / 'user-file').write_text('keep', encoding='utf-8')
                for name in ('framely', 'screenpin'):
                    (commands / name).symlink_to(old / 'screenpin')
            elif previous == 'local':
                (commands / 'screenpin').write_text('old executable', encoding='utf-8')
                (commands / 'framely').symlink_to('screenpin')
                old_workers = local / 'share/screenpin/workers'
                old_workers.mkdir(parents=True)
                (old_workers / 'screenpin-capture').touch()
                desktop.write_text(f'Exec="{commands}/screenpin"\n', encoding='utf-8')
            elif previous == 'unrelated':
                (commands / 'screenpin').write_text('unrelated command', encoding='utf-8')
            # Redirect the test copy only; never change the user's HOME.
            script = original.replace('${XDG_DATA_HOME:-$HOME/.local/share}', str(local / 'share'))
            script = script.replace('${XDG_CONFIG_HOME:-$HOME/.config}', str(home / '.config'))
            script = script.replace('$HOME', str(home))
            installer = source / 'install.sh'
            installer.write_text(script, encoding='utf-8')
            for _ in range(2):
                subprocess.run(['sh', str(installer)], check=True, cwd=tmp)
                app = local / 'framely.app'
                assert (app / 'workers/framely-capture').is_file()
                assert not (app / 'framely').is_symlink()
                assert subprocess.check_output([str(commands / 'framely'), '--version'], text=True).strip() == f'Framely {version} (mygo)'
                assert str(app / 'framely') in desktop.read_text(encoding='utf-8')
                assert 'Hidden=true' in autostart.read_text(encoding='utf-8')
                assert 'X-GNOME-Autostart-enabled=false' in autostart.read_text(encoding='utf-8')
                assert str(app / 'framely') in autostart.read_text(encoding='utf-8')
                if previous == 'unrelated':
                    assert (commands / 'screenpin').read_text(encoding='utf-8') == 'unrelated command'
                else:
                    assert (commands / 'screenpin').resolve() == app / 'framely'
                if shutil.which('desktop-file-validate'):
                    subprocess.run(['desktop-file-validate', str(desktop)], check=True)
            if previous == 'portable':
                assert (local / 'screenpin.app/screenpin').resolve() == app / 'framely'
                assert len(list((local / 'share/framely/install-backups').glob('*/legacy-app/user-file'))) == 1
            subprocess.run(['sh', str(installer), '--uninstall'], check=True)
            assert not app.exists()
            assert not (commands / 'framely').is_symlink()
            assert not desktop.exists()
            assert not (local / 'screenpin.app/screenpin').is_symlink()
            print(f'PASS Linux: {previous}, reinstall, command, desktop, autostart, uninstall', flush=True)


def macos():
    app = ROOT / f'build/native/darwin-{arch}/Framely.app'
    info = plistlib.loads((app / 'Contents/Info.plist').read_bytes())
    assert info['CFBundleExecutable'] == 'Framely', info
    assert info['CFBundleIdentifier'] == 'io.screenpin.desktop', info
    assert info['CFBundleName'] == 'Framely', info
    binary = app / 'Contents/MacOS/Framely'
    assert subprocess.check_output([str(binary), '--version'], text=True).strip() == f'Framely {version} (mygo)'
    assert not (app / 'Contents/MacOS/ScreenPin').exists()
    print('PASS macOS: bundle, executable, version and stable permission ID')


if __name__ == '__main__':
    if system == 'Linux':
        linux()
    elif system == 'Darwin':
        macos()
    else:
        subprocess.run(['powershell', '-NoProfile', '-ExecutionPolicy', 'Bypass', '-File', str(ROOT / 'scripts/check-installers.ps1')], check=True)
