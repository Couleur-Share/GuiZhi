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
KIT = ROOT / "scripts/crawl4ai-acceptance"


def fixture_driver(directory):
    """Playwright driver 的最小替身：验收包只校验 node.exe 与 package/index.mjs 存在并整体复制。"""
    (directory / "package").mkdir(parents=True, exist_ok=True)
    (directory / "node.exe").write_bytes(b"fixture - never executed")
    (directory / "package/index.mjs").write_text("// fixture", encoding="utf-8")


def run_powershell(command):
    return subprocess.run(["powershell.exe", "-NoProfile", "-NonInteractive", "-Command", command],
                          capture_output=True, text=True, encoding="utf-8", errors="replace")


class AcceptanceKitEncodingTest(unittest.TestCase):
    def test_generated_scripts_and_manifest(self):
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary)
            runtime = directory / "runtime"
            for relative in ("site-packages/playwright/driver/package", "site-packages/psutil", "python", "licenses"):
                (runtime / relative).mkdir(parents=True)
            fixture_driver(runtime / "site-packages/playwright/driver")
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

    def test_pruned_runtime_needs_an_explicit_driver_and_declares_the_baseline(self):
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary)
            # 裁剪后的候选运行包：没有 site-packages/playwright/driver，但有 python 与许可证。
            runtime = directory / "runtime"
            for relative in ("python", "licenses"):
                (runtime / relative).mkdir(parents=True)
            (runtime / "THIRD-PARTY-NOTICES.txt").write_text("fixture", encoding="utf-8")
            installer = directory / "installer.exe"
            installer.write_bytes(b"test fixture - never executed")
            driver = directory / "driver"
            fixture_driver(driver)
            command = [sys.executable, str(KIT / "build-kit.py"), "--candidate", str(installer), "--previous", str(installer),
                       "--runtime", str(runtime), "--guest-script", "electron-guest.ps1"]

            # 子进程按本机代码页写出中文错误信息；这里只核对 ASCII 的参数名，解码时容错。
            refused = subprocess.run(command + ["--output", str(directory / "refused")], capture_output=True, text=True,
                                     encoding="utf-8", errors="replace")
            self.assertNotEqual(refused.returncode, 0)
            self.assertIn("--driver", refused.stderr)
            self.assertFalse((directory / "refused").exists(), "缺少 driver 时不应留下半成品验收包")

            output = directory / "kit"
            subprocess.run(command + ["--output", str(output), "--driver", str(driver),
                                      "--previous-baseline", "electron"], check=True, capture_output=True)
            manifest = json.loads((output / "input/manifest.json").read_text("utf-8"))
            self.assertEqual(manifest["previousBaseline"], "electron")
            for name in ("tools/driver/node.exe", "tools/driver/package/index.mjs", "runtime-tree.ps1"):
                self.assertIn(name, manifest["files"])

            legacy = directory / "legacy"
            subprocess.run(command + ["--output", str(legacy), "--driver", str(driver)], check=True, capture_output=True)
            self.assertEqual(json.loads((legacy / "input/manifest.json").read_text("utf-8"))["previousBaseline"],
                             "standalone-chromium")

    @unittest.skipUnless(sys.platform == "win32", "来宾脚本运行在 Windows PowerShell 5.1")
    def test_runtime_tree_matches_manifest_exactly(self):
        with tempfile.TemporaryDirectory() as temporary:
            runtime = Path(temporary) / "crawl4ai"
            (runtime / "site-packages/pkg").mkdir(parents=True)
            (runtime / "site-packages/pkg/a.py").write_text("a", encoding="utf-8")
            (runtime / "python.exe").write_text("p", encoding="utf-8")
            (runtime / "manifest.json").write_text(json.dumps(
                {"files": {"site-packages/pkg/a.py": "0" * 64, "python.exe": "1" * 64}}), encoding="utf-8")
            script = str(KIT / "runtime-tree.ps1").replace("'", "''")
            check = lambda: run_powershell(f". '{script}'; Assert-RuntimeTree '{str(runtime).replace(chr(39), chr(39) * 2)}'")

            passed = check()
            self.assertEqual(passed.returncode, 0, passed.stderr)
            self.assertEqual(passed.stdout.strip(), "2")

            # 覆盖安装残留了旧版依赖（清单里没有的文件）：必须失败并指出文件。
            (runtime / "site-packages/pkg/stale.py").write_text("stale", encoding="utf-8")
            stale = check()
            self.assertNotEqual(stale.returncode, 0)
            self.assertIn("unregistered", stale.stderr)
            self.assertIn("site-packages/pkg/stale.py", stale.stderr)
            (runtime / "site-packages/pkg/stale.py").unlink()

            # 清单里登记了但安装后缺失的文件同样必须失败。
            (runtime / "python.exe").unlink()
            missing = check()
            self.assertNotEqual(missing.returncode, 0)
            self.assertIn("manifest lists", missing.stderr)

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
