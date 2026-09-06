"""离线核对 Windows 候选的实际架构、采集资源和安装包哈希；不替代安装验收。"""

import argparse
import hashlib
import json
from pathlib import Path
import struct


def digest(file):
    with file.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def machine(file):
    with file.open("rb") as stream:
        if stream.read(2) != b"MZ":
            raise ValueError(f"不是 PE 文件: {file}")
        stream.seek(0x3C)
        offset = struct.unpack("<I", stream.read(4))[0]
        stream.seek(offset)
        if stream.read(4) != b"PE\0\0":
            raise ValueError(f"无效 PE 签名: {file}")
        return hex(struct.unpack("<H", stream.read(2))[0])


def check(root, arch):
    unpacked = root / ("win-unpacked" if arch == "x64" else "win-arm64-unpacked")
    resources = unpacked / "resources"
    runtime = resources / "crawl4ai"
    manifest = json.loads((runtime / "manifest.json").read_text(encoding="utf-8"))
    assert manifest["target"] == "win32-x64"
    expected = manifest["files"]
    actual = {p.relative_to(runtime).as_posix() for p in runtime.rglob("*") if p.is_file()}
    assert actual == set(expected) | {"manifest.json"}, "运行包文件集合不匹配"
    for relative, checksum in expected.items():
        assert digest(runtime / relative) == checksum, f"运行包校验失败: {relative}"
    worker = resources / "crawl4ai-worker"
    worker_files = {p.relative_to(worker).as_posix() for p in worker.rglob("*") if p.is_file()}
    assert worker_files == set(manifest["workerHashes"]), "Worker 文件集合不匹配"
    for relative, checksum in manifest["workerHashes"].items():
        assert digest(worker / relative) == checksum, f"Worker 校验失败: {relative}"
    architectures = {
        "electron": machine(unpacked / "GuiZhi.exe"),
        "python": machine(runtime / manifest["python"]),
        "chromium": machine(runtime / manifest["browser"]),
    }
    assert architectures["electron"] == ("0x8664" if arch == "x64" else "0xaa64")
    assert architectures["python"] == architectures["chromium"] == "0x8664"
    installers = list(root.glob(f"GuiZhi-Setup-*-{arch}.exe"))
    assert len(installers) == 1, "安装包缺失或不唯一"
    installer = installers[0]
    return {
        "arch": arch, "architectures": architectures,
        "installer": str(installer.resolve()), "installerBytes": installer.stat().st_size,
        "installerSha256": digest(installer),
        "runtimeFiles": len(expected), "workerFiles": len(worker_files),
        "runtimeBytes": sum(p.stat().st_size for p in runtime.rglob("*") if p.is_file()),
        "resourceIntegrity": "passed", "installedRuntimeAcceptance": "pending",
    }


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--root", type=Path, required=True)
    parser.add_argument("--single-arch", choices=["x64", "arm64"])
    args = parser.parse_args()
    results = [check(args.root, args.single_arch)] if args.single_arch else [check(args.root / arch, arch) for arch in ("x64", "arm64")]
    output = args.root.parent / "package-check.json"
    output.write_text(json.dumps(results, ensure_ascii=False, indent=2), encoding="utf-8")
    print(output)
