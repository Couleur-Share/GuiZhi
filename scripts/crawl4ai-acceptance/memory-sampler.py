"""外置采样器：只读测量指定 GuiZhi 进程树，排除测试驱动和采样器自身。"""
import argparse
import json
from pathlib import Path
import sys
import time

sys.path.insert(0, str(Path(__file__).parent / "tools/metrics"))
import psutil


def sample(root, known):
    processes = {p.pid: p for p in root.children(recursive=True)}
    processes[root.pid] = root
    for pid, process in known.items():
        if process.is_running():
            processes.setdefault(pid, process)
    rows, errors = [], []
    for pid, process in processes.items():
        try:
            memory = process.memory_full_info()
            rows.append({"pid": pid, "name": process.name(), "uss": memory.uss,
                         "private": memory.private, "rss": memory.rss})
            known[pid] = process
        except psutil.NoSuchProcess:
            pass  # 采样期间正常退出的短命进程。
        except psutil.AccessDenied:
            errors.append({"pid": pid, "error": "access-denied"})
    return {"processes": rows, "errors": errors,
            **{key: sum(row[key] for row in rows) for key in ("uss", "private", "rss")}}


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--pid", type=int, required=True)
    parser.add_argument("--out", type=Path, required=True)
    args = parser.parse_args()
    root = psutil.Process(args.pid)
    assert root.name().lower() == "guizhi.exe", "只能采样归知测试实例"
    known = {}
    started = time.monotonic()
    with (args.out / "memory-samples.jsonl").open("w", encoding="utf-8", buffering=1) as stream:
        while not (args.out / "sampler.stop").exists():
            tick = time.monotonic()
            try:
                stage_file = max(args.out.glob("memory-stage-*.json"))
                state = json.loads(stage_file.read_text("utf-8"))
                result = sample(root, known)
            except psutil.NoSuchProcess:
                break
            result.update({"elapsed": tick - started, "stage": state["stage"],
                           "time": time.time(), "sampleSeconds": time.monotonic() - tick})
            stream.write(json.dumps(result) + "\n")
            (args.out / "sampler.ready").touch()
            time.sleep(max(0, 0.25 - (time.monotonic() - tick)))


if __name__ == "__main__":
    main()
