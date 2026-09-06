"""Regression: Windows PowerShell must execute commands after Chinese comments."""
import codecs
import hashlib
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[2]


class AcceptanceKitEncodingTest(unittest.TestCase):
    def test_generated_scripts_and_manifest(self):
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary)
            runtime = directory / "runtime"
            for relative in ("site-packages/playwright/driver", "python", "licenses"):
                (runtime / relative).mkdir(parents=True)
            (runtime / "THIRD-PARTY-NOTICES.txt").write_text("fixture", encoding="utf-8")
            installer = directory / "installer.exe"
            installer.write_bytes(b"test fixture - never executed")
            output = directory / "kit"
            subprocess.run([
                sys.executable, str(ROOT / "scripts/crawl4ai-acceptance/build-kit.py"),
                "--candidate", str(installer), "--previous", str(installer),
                "--runtime", str(runtime), "--output", str(output),
            ], check=True, capture_output=True)
            manifest = json.loads((output / "input/manifest.json").read_text("utf-8"))
            for name, expected in manifest["files"].items():
                self.assertEqual(hashlib.sha256((output / "input" / name).read_bytes()).hexdigest(), expected)
            for script in output.rglob("*.ps1"):
                self.assertTrue(script.read_bytes().startswith(codecs.BOM_UTF8), str(script))
            guest = output / "input/guest.ps1"
            source = ROOT / "scripts/crawl4ai-acceptance/guest.ps1"
            self.assertTrue(source.read_bytes().startswith(codecs.BOM_UTF8))
            self.assertEqual(guest.read_text("utf-8-sig"), source.read_text("utf-8-sig"))
            if sys.platform == "win32":
                self.check_windows_powershell(guest)

    def check_windows_powershell(self, guest):
        # ParseFile uses Windows PowerShell's real BOM/ANSI handling without running installers.
        command = """
$tokens = $null; $errors = $null
$ast = [Management.Automation.Language.Parser]::ParseFile('__PATH__', [ref]$tokens, [ref]$errors)
if (@($errors).Count) { throw 'Parse failed' }
$commands = $ast.FindAll({ param($node) $node -is [Management.Automation.Language.CommandAst] }, $true)
if (@($commands | Where-Object { $_.GetCommandName() -eq 'Copy-Item' }).Count -ne 1) { throw 'Old app copy was swallowed' }
if (@($commands | Where-Object { $_.Extent.Text -eq "Invoke-Shot 'clean-run' 'capture.mjs'" }).Count -ne 1) { throw 'Clean run was swallowed' }
""".replace("__PATH__", str(guest).replace("'", "''"))
        subprocess.run(["powershell.exe", "-NoProfile", "-NonInteractive", "-Command", command], check=True, capture_output=True)


if __name__ == "__main__":
    unittest.main()
