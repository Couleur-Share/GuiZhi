"""从已提交的 URL/哈希锁构建随包组件；此脚本不解析最新版。"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import shutil
import subprocess
import stat
import sys
import tarfile
import urllib.request
import zipfile

ROOT = Path(__file__).resolve().parents[1]
CONFIG = ROOT / "config" / "crawl4ai"


def verify_runtime(python):
    # 英文 Windows 构建机默认 CP1252；自检日志固定 UTF-8，不依赖系统语言。
    subprocess.run([str(python), "-s", "-c", "import crawl4ai,playwright;print('Crawl4AI 随包依赖可加载')"], check=True,
                   env={**os.environ, "PYTHONIOENCODING": "utf-8", "PYTHONUTF8": "1", "CRAWL4_AI_BASE_DIRECTORY": str(CONFIG / "downloads" / "build-cache"), "LITELLM_LOCAL_MODEL_COST_MAP": "True", "HF_HUB_OFFLINE": "1"})


def download(record):
    cache = CONFIG / "downloads"
    cache.mkdir(parents=True, exist_ok=True)
    target = cache / record["sha256"]
    if not target.exists():
        with urllib.request.urlopen(record["url"], timeout=60) as response, target.open("wb") as out:
            shutil.copyfileobj(response, out)
    if hashlib.file_digest(target.open("rb"), "sha256").hexdigest() != record["sha256"]:
        raise RuntimeError("下载校验失败：" + record["url"])
    return target


def main():
    sys.stdout.reconfigure(encoding="utf-8")
    sys.stderr.reconfigure(encoding="utf-8")
    parser = argparse.ArgumentParser()
    parser.add_argument("--target", choices=["win32-x64", "darwin-x64", "darwin-arm64", "linux-x64"], required=True)
    args = parser.parse_args()
    lock = json.loads((CONFIG / "runtime-lock.json").read_text(encoding="utf-8"))
    spec = lock["targets"][args.target]
    host = {"Windows": "win32", "Darwin": "darwin", "Linux": "linux"}[platform.system()]
    arch = "arm64" if platform.machine().lower() in ("arm64", "aarch64") else "x64"
    if args.target != host + "-" + arch:
        raise RuntimeError("必须在目标运行环境构建；Windows ARM64 安装包复用 Windows x64 组件")
    output = ROOT / "apps" / "desktop" / "resources" / "crawl4ai"
    if output.exists():
        raise RuntimeError("输出目录已存在，请保留旧产物并在干净工作区构建")
    output.mkdir(parents=True)
    with tarfile.open(download(spec["python"]), "r:gz") as archive:
        archive.extractall(output, filter="data")
    python = output / spec["pythonExecutable"]
    site = output / "site-packages"
    subprocess.run([str(python), "-m", "pip", "install", "--no-compile", "--only-binary=:all:", "--require-hashes", "--target", str(site), "-r", str(CONFIG / (args.target + ".lock"))], check=True)
    # 随包 Python 使用独立 site-packages，生产不读取系统或用户 Python 配置。
    paths = subprocess.check_output([str(python), "-c", "import sysconfig;print(sysconfig.get_paths()['purelib'])"], text=True).strip()
    Path(paths).mkdir(parents=True, exist_ok=True)
    (Path(paths) / "guizhi-crawl4ai.pth").write_text(os.path.relpath(site, paths) + "\n")
    browser = output / "browser"
    browser.mkdir()
    with zipfile.ZipFile(download(spec["chromium"])) as archive:
        for member in archive.infolist():
            target = (browser / member.filename).resolve()
            if not target.is_relative_to(browser.resolve()):
                raise RuntimeError("浏览器压缩包路径越界")
            mode = member.external_attr >> 16
            if stat.S_ISLNK(mode):
                link = archive.read(member).decode()
                if not (target.parent / link).resolve().is_relative_to(browser.resolve()):
                    raise RuntimeError("浏览器符号链接越界")
                target.parent.mkdir(parents=True, exist_ok=True)
                target.symlink_to(link)
            else:
                archive.extract(member, browser)
                if mode and host != "win32": target.chmod(mode)
    verify_runtime(python)
    licenses = []
    for metadata in site.glob("*.dist-info/METADATA"):
        content = metadata.read_text(encoding="utf-8", errors="replace")
        lines = [line for line in content.splitlines() if line.startswith(("Name:", "Version:", "License:", "License-Expression:", "Classifier: License"))]
        licenses.append("\n".join(lines))
    attribution = "This product includes software developed by UncleCode (https://x.com/unclecode) as part of the Crawl4AI project (https://github.com/unclecode/crawl4ai)."
    (output / "THIRD-PARTY-NOTICES.txt").write_text(attribution + "\n\n" + "\n\n".join(licenses), encoding="utf-8")
    shutil.copytree(CONFIG / "licenses", output / "licenses")
    shutil.copy2(CONFIG / (args.target + ".lock"), output / "requirements.lock")
    files = {}
    for file in sorted(output.rglob("*")):
        if file.is_file():
            with file.open("rb") as source:
                files[file.relative_to(output).as_posix()] = hashlib.file_digest(source, "sha256").hexdigest()
    (output / "manifest.json").write_text(json.dumps(dict(protocol=1, version="0.9.3", target=args.target,
        python=spec["pythonExecutable"], browser="browser/" + spec["browserExecutable"], files=files,
        workerHashes={file.name: hashlib.sha256(file.read_bytes()).hexdigest() for file in (output.parent / "crawl4ai-worker").glob("*.py")}), indent=2), encoding="utf-8")
    print("已构建：" + str(output))


if __name__ == "__main__":
    main()
