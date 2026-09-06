"""英文 Windows 的 CP1252 不能使中文加载自检失败。"""
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest


class BuildEncodingTest(unittest.TestCase):
    def test_runtime_check_overrides_cp1252(self):
        script = Path(__file__).resolve().parents[1] / "build-crawl4ai.py"
        with tempfile.TemporaryDirectory() as directory:
            # 这里只隔离模块依赖，实际执行构建器自检子进程及其中文输出。
            for module in ("crawl4ai", "playwright"):
                (Path(directory) / (module + ".py")).write_text("", encoding="utf-8")
            code = "import runpy,sys;runpy.run_path(sys.argv[1])['verify_runtime'](sys.executable)"
            result = subprocess.run([sys.executable, "-c", code, str(script)], capture_output=True,
                                    env={**os.environ, "PYTHONPATH": directory, "PYTHONIOENCODING": "cp1252", "PYTHONUTF8": "0"})
            self.assertEqual(result.returncode, 0, result.stderr.decode("utf-8", errors="replace"))
            self.assertIn("Crawl4AI 随包依赖可加载", result.stdout.decode("utf-8"))


if __name__ == "__main__":
    unittest.main()
