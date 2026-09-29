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
            # crawler_env() 会清除环境里的 PYTHONPATH（与生产启动一致，防止自检被导向别处的包），
            # 所以桩模块目录由测试显式注入自检子进程；替换要落在函数自己的 __globals__ 上，run_path 返回的是副本。
            code = ("import runpy,sys;g=runpy.run_path(sys.argv[1]);verify=g['verify_runtime'];base=g['crawler_env'];"
                    "verify.__globals__['crawler_env']=lambda:{**base(),'PYTHONPATH':sys.argv[2]};verify(sys.executable)")
            result = subprocess.run([sys.executable, "-c", code, str(script), directory], capture_output=True,
                                    env={**os.environ, "PYTHONIOENCODING": "cp1252", "PYTHONUTF8": "0"})
            self.assertEqual(result.returncode, 0, result.stderr.decode("utf-8", errors="replace"))
            self.assertIn("Crawl4AI 随包依赖可加载", result.stdout.decode("utf-8"))


if __name__ == "__main__":
    unittest.main()
