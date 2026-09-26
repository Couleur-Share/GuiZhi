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
            for relative in ("site-packages/playwright/driver", "site-packages/psutil", "python", "licenses"):
                (runtime / relative).mkdir(parents=True)
            (runtime / "site-packages/psutil/__init__.py").write_text("# fixture", encoding="utf-8")
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
            electron_output = directory / "electron-kit"
            subprocess.run([
                sys.executable, str(ROOT / "scripts/crawl4ai-acceptance/build-kit.py"),
                "--candidate", str(installer), "--previous", str(installer),
                "--runtime", str(runtime), "--output", str(electron_output),
                "--guest-script", "electron-guest.ps1",
                "--candidate-version", "0.25.0", "--previous-version", "0.24.0",
            ], check=True, capture_output=True)
            versions = json.loads((electron_output / "input/manifest.json").read_text("utf-8"))
            self.assertEqual(versions["candidateVersion"], "0.25.0")
            self.assertEqual(versions["previousVersion"], "0.24.0")
            for name in ("launch.ps1", "acceptance.wsb"):
                self.assertIn("\\electron-guest.ps1", (electron_output / name).read_text("utf-8-sig"))
            if sys.platform == "win32":
                self.check_launcher_proxy(electron_output / "launch.ps1")
                script = electron_output / "input/electron-guest.ps1"
                command = "$t=$null;$e=$null;[void][Management.Automation.Language.Parser]::ParseFile('__PATH__',[ref]$t,[ref]$e);if(@($e).Count){throw ($e|Out-String)}"
                subprocess.run(["powershell.exe", "-NoProfile", "-NonInteractive", "-Command",
                                command.replace("__PATH__", str(script).replace("'", "''"))], check=True, capture_output=True)
            memory_output = directory / "memory-kit"
            subprocess.run([
                sys.executable, str(ROOT / "scripts/crawl4ai-acceptance/build-kit.py"),
                "--candidate", str(installer), "--previous", str(installer),
                "--runtime", str(runtime), "--output", str(memory_output),
                "--guest-script", "memory-guest.ps1",
            ], check=True, capture_output=True)
            memory_manifest = json.loads((memory_output / "input/manifest.json").read_text("utf-8"))
            self.assertIn("tools/metrics/psutil/__init__.py", memory_manifest["files"])
            self.assertIn("\\memory-guest.ps1", (memory_output / "acceptance.wsb").read_text("utf-8-sig"))
            if sys.platform == "win32":
                script = memory_output / "input/memory-guest.ps1"
                subprocess.run(["powershell.exe", "-NoProfile", "-NonInteractive", "-Command",
                                command.replace("__PATH__", str(script).replace("'", "''"))], check=True, capture_output=True)

    def check_launcher_proxy(self, launcher):
        # 模拟启动失败，验证代理既不传给沙盒，也不会被永久清除。
        command = """
$env:HTTP_PROXY='http://proxy.invalid:1'
$env:HTTPS_PROXY='http://proxy.invalid:2'
$env:ALL_PROXY='socks5://proxy.invalid:3'
$global:launchChecked=$false
function Start-Process {
  param($FilePath,$ArgumentList,$WindowStyle)
  if ($env:HTTP_PROXY -or $env:HTTPS_PROXY -or $env:ALL_PROXY) { throw 'Proxy inherited' }
  if ($FilePath -notlike '*WindowsSandbox.exe' -or $WindowStyle -ne 'Hidden') { throw 'Wrong launcher' }
  if ($ArgumentList -notmatch '^".*acceptance.wsb"$') { throw 'Configuration path not quoted' }
  $global:launchChecked=$true
  throw 'Simulated launch failure'
}
try { & '__PATH__'; throw 'Expected failure' }
catch { if ($_.Exception.Message -ne 'Simulated launch failure') { throw } }
if (!$global:launchChecked) { throw 'Launcher was not called' }
if ($env:HTTP_PROXY -ne 'http://proxy.invalid:1' -or $env:HTTPS_PROXY -ne 'http://proxy.invalid:2' -or $env:ALL_PROXY -ne 'socks5://proxy.invalid:3') { throw 'Proxy was not restored' }
exit 0
""".replace("__PATH__", str(launcher).replace("'", "''"))
        subprocess.run(["powershell.exe", "-NoProfile", "-NonInteractive", "-Command", command], check=True, capture_output=True)

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
