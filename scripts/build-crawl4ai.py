"""从已提交的 URL/哈希锁构建随包组件；此脚本不解析最新版。"""
import argparse
import csv
import hashlib
import json
import os
from pathlib import Path
import platform
import re
import shutil
import subprocess
import sys
import tarfile
import urllib.request

ROOT = Path(__file__).resolve().parents[1]
CONFIG = ROOT / "config" / "crawl4ai"
WORKER = ROOT / "apps" / "desktop" / "resources" / "crawl4ai-worker"

# 归知只调用 Crawl4AI 的 HTML 清洗与 Markdown 生成，页面渲染由 Electron 完成；
# 下列内容在这条提取路径上从不加载或执行，安装后删除以缩小安装包与每次采集前的完整性校验。
# 依据：导入 content_scraping_strategy 与 markdown_generation_strategy 后，sys.modules 不含这些包；
# playwright / patchright 的 driver 目录各自携带一份约 88 MiB 的 node.exe，只在启动它们自己的浏览器时使用，
# 而 Python 模块本身仍由 Crawl4AI 导入，必须保留。
# 名称是 pip distribution 名（unclecode_litellm 的导入名为 litellm），按 RECORD 精确卸载。
# 锁文件保持完整（--require-hashes 要求依赖闭包全部在锁内），裁剪发生在安装之后，
# 由 verify_runtime / verify_extraction 兜底：Crawl4AI 升级后一旦开始导入这些包，构建会当场失败。
# 体积上限见 config/package-size-budget.json。
PRUNED_DISTRIBUTIONS = ("hf_xet", "networkx", "nltk", "openai", "scipy", "tokenizers", "unclecode_litellm")
PRUNED_SUBDIRECTORIES = ("playwright/driver", "patchright/driver")

# 与 extract.py 的真实调用路径一致：中文、表格、代码块、链接与页面噪声。
EXTRACTION_SMOKE_HTML = (
    "<html><head><title>归知自检</title></head><body><nav><a href='/'>首页</a></nav>"
    "<article><h1>归知正文</h1><p>本地优先的知识库，详见<a href='https://example.com/guide'>指南</a>。</p>"
    "<table><thead><tr><th>层</th><th>选择</th></tr></thead><tbody><tr><td>数据库</td><td>SQLite</td></tr></tbody></table>"
    "<pre><code class='language-python'>print(add(1, 2))</code></pre></article><footer>版权所有</footer></body></html>"
)
EXTRACTION_SMOKE_EXPECTED = ("归知正文", "SQLite", "print(add(1, 2))", "https://example.com/guide")
EXTRACTION_SMOKE_CODE = """
import sys
sys.path.insert(0, sys.argv[1])
from extract import extract
result = extract(sys.argv[2], "https://example.com/guizhi", 200)
assert result["complete"], result
missing = [text for text in sys.argv[3:] if text not in result["markdown"]]
assert not missing, ("提取结果缺少", missing, result["markdown"])
print("Crawl4AI 正文提取自检通过")
"""


def crawler_env():
    """自检子进程的环境：与生产启动一致，清除 PYTHONPATH / PYTHONHOME。

    开发者环境若带着指向其它运行包的 PYTHONPATH，随包 Python 会优先从那里导入，
    已裁剪的包便“重新可导入”，自检在错误的目录上空转通过。
    """
    env = {key: value for key, value in os.environ.items() if key not in ("PYTHONPATH", "PYTHONHOME")}
    # 英文 Windows 构建机默认 CP1252；自检日志固定 UTF-8，不依赖系统语言。
    env.update(PYTHONIOENCODING="utf-8", PYTHONUTF8="1", CRAWL4_AI_BASE_DIRECTORY=str(CONFIG / "downloads" / "build-cache"),
               LITELLM_LOCAL_MODEL_COST_MAP="True", HF_HUB_OFFLINE="1")
    return env


def verify_runtime(python):
    subprocess.run([str(python), "-B", "-s", "-c", "import crawl4ai,playwright;print('Crawl4AI 随包依赖可加载')"], check=True, env=crawler_env())


def verify_extraction(python):
    """用随包 Python 执行真实的 extract.py，证明裁剪后提取路径仍完整可用。"""
    subprocess.run([str(python), "-B", "-s", "-c", EXTRACTION_SMOKE_CODE, str(WORKER), EXTRACTION_SMOKE_HTML, *EXTRACTION_SMOKE_EXPECTED],
                   check=True, env=crawler_env())


def normalize_distribution(name):
    return re.sub(r"[-_.]+", "_", name).lower()


def directory_size(path):
    return sum(file.stat().st_size for file in path.rglob("*") if file.is_file())


def remove_distribution(site, info):
    """按 dist-info/RECORD 卸载一个 distribution（含 scipy.libs 之类的附属目录），返回释放字节数。"""
    root = os.path.normpath(site)
    freed, parents = 0, set()
    # 先读完并关闭 RECORD：它登记了自身，Windows 不允许删除仍被打开的文件。
    with (info / "RECORD").open(encoding="utf-8", newline="") as record:
        rows = list(csv.reader(record))
    for row in rows:
        if not row:
            continue
        file = os.path.normpath(os.path.join(root, row[0]))
        # RECORD 可能登记 ../../Scripts 之类落在 site-packages 之外的入口，绝不触碰。
        if not file.startswith(root + os.sep) or not os.path.isfile(file):
            continue
        freed += os.path.getsize(file)
        os.remove(file)
        parents.add(os.path.dirname(file))
    for parent in sorted(parents, key=len, reverse=True):
        while parent != root and os.path.isdir(parent) and not os.listdir(parent):
            os.rmdir(parent)
            parent = os.path.dirname(parent)
    # RECORD 登记了 METADATA 等自身文件，上面的清理可能已把 dist-info 删空；剩下的未登记文件一并删除。
    if info.is_dir():
        freed += directory_size(info)
        shutil.rmtree(info)
    return freed


def prune_runtime(site, distributions=PRUNED_DISTRIBUTIONS, subdirectories=PRUNED_SUBDIRECTORIES):
    """删除提取路径从不使用的 distribution 与 driver 目录，返回 (被删除项, 释放字节数)；不存在的项跳过。"""
    removed, freed = [], 0
    wanted = {normalize_distribution(name) for name in distributions}
    for info in sorted(site.glob("*.dist-info")):
        if normalize_distribution(info.name[: -len(".dist-info")].rpartition("-")[0]) in wanted:
            freed += remove_distribution(site, info)
            removed.append(info.name[: -len(".dist-info")])
    for relative in subdirectories:
        target = site / relative
        if target.is_dir():
            freed += directory_size(target)
            shutil.rmtree(target)
            removed.append(relative)
    return removed, freed


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
    parser.add_argument("--output", type=Path, help="独立验证输出目录；必须尚不存在")
    args = parser.parse_args()
    lock = json.loads((CONFIG / "runtime-lock.json").read_text(encoding="utf-8"))
    spec = lock["targets"][args.target]
    host = {"Windows": "win32", "Darwin": "darwin", "Linux": "linux"}[platform.system()]
    arch = "arm64" if platform.machine().lower() in ("arm64", "aarch64") else "x64"
    if args.target != host + "-" + arch:
        raise RuntimeError("必须在目标运行环境构建；Windows ARM64 安装包复用 Windows x64 组件")
    output = (args.output or ROOT / "apps" / "desktop" / "resources" / "crawl4ai").resolve()
    if output.exists():
        raise RuntimeError("输出目录已存在，请保留旧产物并在干净工作区构建")
    output.mkdir(parents=True)
    with tarfile.open(download(spec["python"]), "r:gz") as archive:
        archive.extractall(output, filter="data")
    python = output / spec["pythonExecutable"]
    site = output / "site-packages"
    subprocess.run([str(python), "-B", "-m", "pip", "install", "--no-compile", "--only-binary=:all:", "--require-hashes", "--target", str(site), "-r", str(CONFIG / (args.target + ".lock"))], check=True)
    # 随包 Python 使用独立 site-packages，生产不读取系统或用户 Python 配置。
    paths = subprocess.check_output([str(python), "-B", "-c", "import sysconfig;print(sysconfig.get_paths()['purelib'])"], text=True).strip()
    Path(paths).mkdir(parents=True, exist_ok=True)
    (Path(paths) / "guizhi-crawl4ai.pth").write_text(os.path.relpath(site, paths) + "\n")
    # 页面渲染复用 Electron；Playwright Python 模块仍是 Crawl4AI 的导入依赖。
    removed, freed = prune_runtime(site)
    print(f"已裁剪 {len(removed)} 项提取路径不使用的内容，释放 {freed / 1024 / 1024:.1f} MiB")
    verify_runtime(python)
    verify_extraction(python)
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
        python=spec["pythonExecutable"], renderer="electron", files=files,
        workerHashes={file.name: hashlib.sha256(file.read_bytes()).hexdigest() for file in (ROOT / "apps/desktop/resources/crawl4ai-worker").glob("*.py")}), indent=2), encoding="utf-8")
    print("已构建：" + str(output))


if __name__ == "__main__":
    main()
