"""Exercise local command migration without touching the user's installation."""
import contextlib
import io
import json
import os
from pathlib import Path
import runpy
import shutil
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import Mock, patch

ROOT = Path(__file__).resolve().parents[1]


@unittest.skipUnless(sys.platform == "linux", "Linux installer")
class LocalInstallTests(unittest.TestCase):
    def test_command_and_desktop_entry_survive_install_and_migration(self):
        for previous in ("missing", "file", "symlink", "broken-symlink"):
            with self.subTest(previous=previous), tempfile.TemporaryDirectory() as directory:
                root = Path(directory) / "repo"
                home = Path(directory) / "user home"
                script = root / "scripts/install-local-linux.py"
                script.parent.mkdir(parents=True)
                shutil.copy2(ROOT / "scripts/install-local-linux.py", script)
                for name in ("mygo.json", "assets/icons/128x128.png",
                             "packaging/arch-native/io.screenpin.desktop.desktop"):
                    target = root / name
                    target.parent.mkdir(parents=True, exist_ok=True)
                    shutil.copy2(ROOT / name, target)
                version = json.loads((root / "mygo.json").read_text(encoding="utf-8"))["version"]
                expected = f"Framely {version} (mygo)"
                bundle = root / "build/native/linux-amd64"
                workers = bundle / "workers"
                (workers / "framely-ocr").mkdir(parents=True)
                (workers / "framely-capture").touch()
                (workers / "framely-ocr/framely-ocr").touch()
                source = bundle / "framely"
                source.write_text(f"#!/bin/sh\nprintf '%s\\n' '{expected}'\n", encoding="utf-8")
                source.chmod(0o755)

                binary = home / ".local/bin/screenpin"
                binary.parent.mkdir(parents=True)
                binary.write_text("old executable", encoding="utf-8")
                command = binary.with_name("framely")
                if previous == "file":
                    command.write_text("old command", encoding="utf-8")
                elif previous in ("symlink", "broken-symlink"):
                    command.symlink_to("screenpin" if previous == "symlink" else "missing")
                applications = home / ".local/share/applications"
                applications.mkdir(parents=True)
                legacy = applications / "screenpin.desktop"
                legacy.write_text("old desktop", encoding="utf-8")
                installed_workers = home / ".local/share/screenpin/workers"
                installed_workers.mkdir(parents=True)
                (installed_workers / "old-worker").touch()
                autostart = home / '.config/autostart/io.screenpin.desktop.desktop'
                autostart.parent.mkdir(parents=True)
                autostart.write_text('[Desktop Entry]\nName=ScreenPin\nExec=old --hidden\nHidden=true\nX-GNOME-Autostart-enabled=false\n', encoding='utf-8')

                process = Mock(pid=1234)
                process.poll.return_value = None
                with patch.object(sys, "argv", [str(script)]), \
                        patch.dict(os.environ, {'XDG_CONFIG_HOME': str(home / '.config')}), \
                        patch.object(Path, "home", return_value=home), \
                        patch("platform.machine", return_value="x86_64"), \
                        patch("subprocess.check_output", return_value=expected + "\n"), \
                        patch("subprocess.run", return_value=subprocess.CompletedProcess([], 1, stdout="")), \
                        patch("subprocess.Popen", return_value=process) as launch, \
                        patch("shutil.which", return_value=None), \
                        patch("time.sleep"), contextlib.redirect_stdout(io.StringIO()):
                    runpy.run_path(str(script), run_name="__main__")

                self.assertFalse(command.is_symlink())
                self.assertEqual(binary.readlink(), Path("framely"))
                for executable in (command, binary):
                    self.assertEqual(subprocess.check_output([str(executable), "--version"], text=True).strip(), expected)
                entry = (applications / "io.screenpin.desktop.desktop").read_text(encoding="utf-8")
                self.assertIn(f'\nExec="{command}"\n', entry)
                self.assertFalse(legacy.exists())
                self.assertTrue((home / '.local/share/framely/workers/framely-capture').is_file())
                self.assertFalse((installed_workers / "old-worker").exists())
                self.assertEqual(launch.call_args.args[0], [str(command), "--scroll"])
                backup, = (root / "build/local-install-backup").iterdir()
                self.assertEqual((backup / "screenpin").read_text(encoding="utf-8"), "old executable")
                self.assertTrue((backup / "legacy-workers/old-worker").is_file())
                self.assertIn(f'Exec="{command}" --hidden', autostart.read_text(encoding='utf-8'))
                self.assertIn('Hidden=true', autostart.read_text(encoding='utf-8'))
                self.assertIn('X-GNOME-Autostart-enabled=false', autostart.read_text(encoding='utf-8'))
                if previous == "file":
                    self.assertEqual((backup / "framely").read_text(encoding="utf-8"), "old command")
                elif previous in ("symlink", "broken-symlink"):
                    self.assertEqual((backup / "framely").readlink(), Path("screenpin" if previous == "symlink" else "missing"))


if __name__ == "__main__":
    unittest.main()
