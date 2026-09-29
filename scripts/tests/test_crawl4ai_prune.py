"""随包运行包裁剪：按 RECORD 精确卸载、不越出 site-packages、幂等，并保留 Crawl4AI 仍会导入的模块。"""
import os
from pathlib import Path
import re
import runpy
import tempfile
import unittest
from unittest import mock

SCRIPT = Path(__file__).resolve().parents[1] / "build-crawl4ai.py"
BUILD = runpy.run_path(str(SCRIPT))


def write(path, content="x"):
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(content, encoding="utf-8")


def install(site, dist, version, files, extra_record=()):
    """伪造一个 distribution：files 为相对 site 的文件，extra_record 为额外登记的 RECORD 行。"""
    info = site / f"{dist}-{version}.dist-info"
    for relative, content in files.items():
        write(site / relative, content)
    write(info / "METADATA", f"Name: {dist}\nVersion: {version}\n")
    rows = [f"{relative},sha256=x,{len(content)}" for relative, content in files.items()]
    rows += [f"{info.name}/METADATA,,", f"{info.name}/RECORD,,", *extra_record]
    write(info / "RECORD", "\n".join(rows) + "\n")


class PruneRuntimeTest(unittest.TestCase):
    def setUp(self):
        directory = tempfile.TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        self.root = Path(directory.name)
        self.site = self.root / "runtime" / "site-packages"
        self.site.mkdir(parents=True)

    def prune(self, distributions, subdirectories=()):
        return BUILD["prune_runtime"](self.site, distributions, subdirectories)

    def test_removes_distribution_with_companion_directory_and_metadata(self):
        install(self.site, "scipy", "1.0", {"scipy/__init__.py": "12345", "scipy.libs/blas.dll": "1234567890"})
        install(self.site, "keep", "1.0", {"keep/__init__.py": "kept"})
        removed, freed = self.prune(("scipy",))
        self.assertEqual(removed, ["scipy-1.0"])
        self.assertFalse((self.site / "scipy").exists())
        self.assertFalse((self.site / "scipy.libs").exists(), "RECORD 登记的 .libs 附属目录必须一并删除")
        self.assertFalse((self.site / "scipy-1.0.dist-info").exists())
        self.assertTrue((self.site / "keep/__init__.py").is_file())
        self.assertTrue((self.site / "keep-1.0.dist-info/RECORD").is_file())
        # 5 + 10 字节文件，加上 dist-info 内的 METADATA 与 RECORD。
        self.assertGreaterEqual(freed, 15)

    def test_matches_distribution_name_not_import_name(self):
        # 上游 litellm 分支的 distribution 是 unclecode_litellm，导入名却是 litellm。
        install(self.site, "unclecode_litellm", "1.0", {"litellm/__init__.py": "x", "litellm/utils/a.py": "y"})
        removed, _ = self.prune(("unclecode-litellm",))
        self.assertEqual(removed, ["unclecode_litellm-1.0"])
        self.assertFalse((self.site / "litellm").exists())

    def test_never_touches_files_outside_site_packages(self):
        # 哨兵文件都在临时目录内；即使守卫失效，也不会波及真实系统文件。
        scripts = self.root / "runtime" / "Scripts" / "tool.exe"
        sibling = self.root / "runtime" / "site-packages-extra" / "data.txt"
        write(scripts, "keep me")
        write(sibling, "keep me too")
        install(self.site, "openai", "1.0", {"openai/__init__.py": "x"},
                extra_record=("../Scripts/tool.exe,,", "../site-packages-extra/data.txt,,"))
        self.prune(("openai",))
        self.assertTrue(scripts.is_file(), "RECORD 指向 site-packages 之外的文件不得删除")
        self.assertTrue(sibling.is_file(), "仅前缀相同的兄弟目录也不属于 site-packages")
        self.assertFalse((self.site / "openai").exists())

    def test_keeps_shared_directories_still_used_by_other_distributions(self):
        install(self.site, "a", "1.0", {"shared/a.py": "a"})
        install(self.site, "b", "1.0", {"shared/b.py": "b"})
        self.prune(("a",))
        self.assertFalse((self.site / "shared/a.py").exists())
        self.assertTrue((self.site / "shared/b.py").is_file(), "命名空间目录仍有其他 distribution 的文件时不能删除")

    def test_removes_only_driver_directories_and_keeps_importable_modules(self):
        write(self.site / "playwright/__init__.py", "import")
        write(self.site / "playwright/driver/node.exe", "N" * 100)
        write(self.site / "patchright/driver/package/cli.js", "js")
        write(self.site / "patchright/__init__.py", "import")
        removed, freed = self.prune((), ("playwright/driver", "patchright/driver"))
        self.assertEqual(sorted(removed), ["patchright/driver", "playwright/driver"])
        self.assertGreaterEqual(freed, 102)
        self.assertTrue((self.site / "playwright/__init__.py").is_file(), "Crawl4AI 仍导入 playwright 模块，Python 部分必须保留")
        self.assertTrue((self.site / "patchright/__init__.py").is_file())

    def test_is_idempotent_and_skips_missing_entries(self):
        install(self.site, "nltk", "1.0", {"nltk/__init__.py": "x"})
        self.prune(("nltk", "not-installed"), ("playwright/driver",))
        self.assertEqual(self.prune(("nltk", "not-installed"), ("playwright/driver",)), ([], 0))

    def test_default_lists_cover_measured_heavy_dependencies(self):
        # 这些是 2026-09-29 实测“提取路径从不加载”的大件；改动清单时需重新做导入追踪与体积核对。
        self.assertEqual(
            set(BUILD["PRUNED_DISTRIBUTIONS"]),
            {"hf_xet", "networkx", "nltk", "openai", "scipy", "tokenizers", "unclecode_litellm"},
        )
        self.assertEqual(set(BUILD["PRUNED_SUBDIRECTORIES"]), {"playwright/driver", "patchright/driver"})

    def test_pruned_distributions_are_still_locked_for_the_official_target(self):
        # 依赖升级若移除或改名了被裁剪的包，裁剪会静默落空、运行包悄悄变大；
        # 在 PR 阶段读锁文件就能发现，不必等到发版构建才被体积门禁拦下。
        normalize = BUILD["normalize_distribution"]
        lock = (BUILD["CONFIG"] / "win32-x64.lock").read_text(encoding="utf-8")
        locked = {normalize(name) for name in re.findall(r"^([A-Za-z0-9][A-Za-z0-9._-]*)==", lock, re.MULTILINE)}
        wanted = {normalize(name) for name in BUILD["PRUNED_DISTRIBUTIONS"]}
        self.assertLessEqual(wanted, locked, f"锁文件里找不到 {sorted(wanted - locked)}：请核对 PRUNED_DISTRIBUTIONS 是否随依赖升级改名或已移除")


class CrawlerEnvTest(unittest.TestCase):
    def test_drops_python_path_variables_that_could_redirect_imports(self):
        # 实测：PYTHONPATH 指向另一份运行包时，随包 Python 能导入已被裁剪的 litellm，自检便在错误的目录上通过。
        leaked = {"PYTHONPATH": "C:/other/site-packages", "PYTHONHOME": "C:/other/python", "GUIZHI_KEEP": "1"}
        with mock.patch.dict(os.environ, leaked):
            env = BUILD["crawler_env"]()
        self.assertNotIn("PYTHONPATH", env)
        self.assertNotIn("PYTHONHOME", env)
        self.assertEqual(env["GUIZHI_KEEP"], "1")
        self.assertEqual((env["PYTHONUTF8"], env["HF_HUB_OFFLINE"]), ("1", "1"))


if __name__ == "__main__":
    unittest.main()
