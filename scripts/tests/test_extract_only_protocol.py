"""extract-only.py 的 stdio 协议回归：提取阶段的异常必须按协议回报，且进程继续服务后续请求。

用桩 extract 模块代替 Crawl4AI，只依赖标准库，可在没有随包 Python 的环境（PR 阶段）运行。
真实转换链路下的协议成功路径见 test_article_cleanup.py::test_offline_worker_protocol（需随包 Python）。
"""
import json
import os
import shutil
import subprocess
import sys
import tempfile
import threading
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
WORKER = ROOT / "apps/desktop/resources/crawl4ai-worker/extract-only.py"
STUB = '''
def extract(html, url, status):
    if "boom" in html:
        raise TypeError("桩：页面触发的提取器缺陷；异常消息可能含页面内容，不应被转发")
    return {"markdown": html, "complete": True, "paragraphs": []}
'''
TIMEOUT_SECONDS = 30


class WorkerSession:
    """把 extract-only.py 与桩 extract.py 放进临时目录后启动，脚本目录优先于其他路径，桩必被导入。"""

    def __enter__(self):
        self.directory = Path(tempfile.mkdtemp(prefix="guizhi-extract-only-"))
        shutil.copyfile(WORKER, self.directory / "extract-only.py")
        (self.directory / "extract.py").write_text(STUB, encoding="utf-8")
        self.stderr = open(self.directory / "stderr.log", "wb")
        environment = {key: value for key, value in os.environ.items() if key not in ("PYTHONPATH", "PYTHONHOME")}
        self.process = subprocess.Popen(
            [sys.executable, "-B", "-s", str(self.directory / "extract-only.py")],
            stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=self.stderr, cwd=self.directory, env=environment,
        )
        # 协议卡住时由看门狗结束进程，readline 随之返回空，避免测试无限挂起。
        self.watchdog = threading.Timer(TIMEOUT_SECONDS, self.process.kill)
        self.watchdog.start()
        return self

    def __exit__(self, *exc):
        self.watchdog.cancel()
        if self.process.poll() is None:
            self.process.kill()
        self.process.wait()
        self.process.stdin.close()
        self.process.stdout.close()
        self.stderr.close()
        shutil.rmtree(self.directory, ignore_errors=True)

    def read(self):
        line = self.process.stdout.readline()
        if not line:
            detail = (self.directory / "stderr.log").read_text(encoding="utf-8", errors="replace")
            raise AssertionError(f"协议无响应或进程已退出（返回码 {self.process.poll()}）：{detail[-500:]}")
        return json.loads(line.decode("utf-8"))

    def send(self, raw):
        self.process.stdin.write(raw if isinstance(raw, bytes) else json.dumps(raw).encode("utf-8") + b"\n")
        self.process.stdin.flush()

    def ask(self, **message):
        self.send(dict(v=1, status=200, url="https://example.com/", **message))
        return self.read()


class ExtractOnlyProtocolTests(unittest.TestCase):
    def test_extractor_exception_is_reported_and_process_keeps_serving(self):
        with WorkerSession() as worker:
            self.assertEqual(worker.read(), dict(v=1, type="ready"))
            self.assertEqual(worker.ask(id="a", html="正文")["result"]["markdown"], "正文")

            # 只回报异常类型名；桩的异常消息（可能含页面内容）不得出现在协议输出里。
            self.assertEqual(worker.ask(id="b", html="boom"), dict(v=1, id="b", error="TypeError"))
            self.assertIsNone(worker.process.poll(), "提取异常不应结束进程")

            self.assertEqual(worker.ask(id="c", html="仍可继续")["result"]["markdown"], "仍可继续")

    def test_malformed_and_mismatched_frames_are_reported_without_exiting(self):
        with WorkerSession() as worker:
            self.assertEqual(worker.read(), dict(v=1, type="ready"))

            worker.send(b"not json\n")
            self.assertEqual(worker.read(), dict(v=1, id="", error="JSONDecodeError"))

            # 协议版本不符：保留请求编号，便于主进程对应到具体任务。
            worker.send(dict(v=2, id="d", status=200, url="https://example.com/", html="x"))
            self.assertEqual(worker.read(), dict(v=1, id="d", error="ValueError"))

            self.assertEqual(worker.ask(id="e", html="恢复")["result"]["markdown"], "恢复")

    def test_closing_stdin_ends_the_process_cleanly(self):
        with WorkerSession() as worker:
            self.assertEqual(worker.read(), dict(v=1, type="ready"))
            worker.process.stdin.close()
            self.assertEqual(worker.process.wait(timeout=TIMEOUT_SECONDS), 0)


if __name__ == "__main__":
    unittest.main()
