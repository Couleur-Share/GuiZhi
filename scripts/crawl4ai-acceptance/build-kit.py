"""生成离线 Windows Sandbox 验收包。只复制产物，不启动或安装应用。"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
from xml.sax.saxutils import escape


def digest(file):
    with file.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--candidate", type=Path, required=True)
    parser.add_argument("--previous", type=Path, required=True)
    parser.add_argument("--runtime", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    root = args.output.resolve()
    assert not root.exists(), "验收包输出已存在，请使用新目录保留历史结果"
    source = Path(__file__).resolve().parent
    inputs, outputs = root / "input", root / "output"
    inputs.mkdir(parents=True)
    outputs.mkdir()
    shutil.copyfile(args.candidate, inputs / "candidate.exe")
    shutil.copyfile(args.previous, inputs / "previous.exe")
    for file in source.iterdir():
        if file.suffix in (".mjs", ".ps1", ".py") and file.name != "build-kit.py":
            if file.suffix == ".ps1":
                # Windows PowerShell 5.1 needs a BOM to preserve Chinese comments.
                (inputs / file.name).write_text(
                    file.read_text(encoding="utf-8-sig"),
                    encoding="utf-8-sig", newline="\r\n",
                )
            else:
                shutil.copyfile(file, inputs / file.name)
    shutil.copyfile(source.parent.parent / "apps/desktop/scripts/screenshot.mjs", inputs / "screenshot.mjs")
    shutil.copytree(args.runtime / "site-packages/playwright/driver", inputs / "tools/driver")
    shutil.copytree(args.runtime / "python", inputs / "tools/python")
    shutil.copytree(args.runtime / "licenses", inputs / "licenses")
    shutil.copyfile(args.runtime / "THIRD-PARTY-NOTICES.txt", inputs / "THIRD-PARTY-NOTICES.txt")
    manifest = {"buildHost": os.environ.get("COMPUTERNAME"), "files": {p.relative_to(inputs).as_posix(): digest(p) for p in sorted(inputs.rglob("*")) if p.is_file()}}
    (inputs / "manifest.json").write_text(json.dumps(manifest, indent=2), encoding="utf-8")
    # 转移到另一台主机后，可直接运行同目录 launch.ps1 重新生成绝对路径映射。
    launcher = '''$ErrorActionPreference = 'Stop'
$inputPath = [Security.SecurityElement]::Escape((Join-Path $PSScriptRoot 'input'))
$outputPath = [Security.SecurityElement]::Escape((Join-Path $PSScriptRoot 'output'))
@"
<Configuration>
  <Networking>Disable</Networking><vGPU>Disable</vGPU><MemoryInMB>8192</MemoryInMB>
  <AudioInput>Disable</AudioInput><VideoInput>Disable</VideoInput><ClipboardRedirection>Disable</ClipboardRedirection><PrinterRedirection>Disable</PrinterRedirection>
  <MappedFolders>
    <MappedFolder><HostFolder>$inputPath</HostFolder><SandboxFolder>C:\\GuiZhiAcceptanceInput</SandboxFolder><ReadOnly>true</ReadOnly></MappedFolder>
    <MappedFolder><HostFolder>$outputPath</HostFolder><SandboxFolder>C:\\GuiZhiAcceptanceOutput</SandboxFolder><ReadOnly>false</ReadOnly></MappedFolder>
  </MappedFolders>
  <LogonCommand><Command>powershell.exe -NoProfile -ExecutionPolicy Bypass -File C:\\GuiZhiAcceptanceInput\\guest.ps1</Command></LogonCommand>
</Configuration>
"@ | Set-Content -LiteralPath (Join-Path $PSScriptRoot 'acceptance.wsb') -Encoding UTF8
Start-Process -FilePath (Join-Path $PSScriptRoot 'acceptance.wsb')
'''
    (root / "launch.ps1").write_text(launcher, encoding="utf-8-sig", newline="\r\n")
    configuration = f'''<Configuration>
  <Networking>Disable</Networking><vGPU>Disable</vGPU><MemoryInMB>8192</MemoryInMB>
  <AudioInput>Disable</AudioInput><VideoInput>Disable</VideoInput><ClipboardRedirection>Disable</ClipboardRedirection><PrinterRedirection>Disable</PrinterRedirection>
  <MappedFolders>
    <MappedFolder><HostFolder>{escape(str(inputs))}</HostFolder><SandboxFolder>C:\\GuiZhiAcceptanceInput</SandboxFolder><ReadOnly>true</ReadOnly></MappedFolder>
    <MappedFolder><HostFolder>{escape(str(outputs))}</HostFolder><SandboxFolder>C:\\GuiZhiAcceptanceOutput</SandboxFolder><ReadOnly>false</ReadOnly></MappedFolder>
  </MappedFolders>
  <LogonCommand><Command>powershell.exe -NoProfile -ExecutionPolicy Bypass -File C:\\GuiZhiAcceptanceInput\\guest.ps1</Command></LogonCommand>
</Configuration>'''
    (root / "acceptance.wsb").write_text(configuration, encoding="utf-8-sig", newline="\r\n")
    (root / "README.txt").write_text("Windows 11 x64 offline acceptance kit. NOT YET GUEST-VERIFIED.\nEnable Windows Sandbox on a disposable test host, then run launch.ps1. It installs only inside the guest.\nInput is read-only; only output is writable to the host. Internet, clipboard, microphone and camera are disabled.\nResults: output/run-*/result.json, screenshots, logs and synthetic databases. Failures remain preserved.\nDo not execute guest.ps1 on a daily-use computer. For a disposable VM, supply -DisposableVM and explicit InputRoot/OutputRoot.\n", encoding="utf-8")
    shutil.copyfile(source.parent.parent / "docs/crawl4ai-p5-windows.md", root / "README.zh-CN.md")
    print(root)
