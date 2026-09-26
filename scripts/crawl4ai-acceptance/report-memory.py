"""从原始进程采样生成内存对照；不将两轮短测解释为长期无泄漏保证。"""
import argparse
from collections import defaultdict
import hashlib
import json
from pathlib import Path
import statistics

MIB = 1024 * 1024


def read(path):
    return json.loads(path.read_text("utf-8-sig"))


def phase_report(folder):
    result = read(folder / "memory-result.json")
    assert result["passed"] and len(result["captures"]) == 13
    raw_rows = [json.loads(line) for line in (folder / "memory-samples.jsonl").read_text("utf-8").splitlines()]
    invalid = [row for row in raw_rows if row["errors"]]
    # Windows 短命进程退出附近可能返回 AccessDenied。保留原始行并显式排除不完整总量。
    assert raw_rows and len(invalid) / len(raw_rows) < 0.01, "不完整内存采样达到 1%，需要重测"
    rows = [row for row in raw_rows if not row["errors"]]
    by_stage = defaultdict(list)
    for row in rows:
        assert any(p["pid"] == result["runtime"]["pid"] for p in row["processes"])
        by_stage[row["stage"]].append(row)
    stages = {}
    for stage, samples in by_stage.items():
        stage_invalid = [row for row in invalid if row["stage"] == stage]
        assert len(stage_invalid) / (len(samples) + len(stage_invalid)) < 0.1, f"阶段采样缺失过多：{stage}"
        peak = max(samples, key=lambda row: row["uss"])
        stages[stage] = {
            "samples": len(samples), "excludedSamples": len(stage_invalid), "ussMedianMiB": round(statistics.median(r["uss"] for r in samples) / MIB, 2),
            "mainUssMedianMiB": round(statistics.median(next(p["uss"] for p in r["processes"] if p["pid"] == result["runtime"]["pid"]) for r in samples) / MIB, 2),
            "ussPeakMiB": round(peak["uss"] / MIB, 2),
            "privateCommitPeakMiB": round(max(r["private"] for r in samples) / MIB, 2),
            "peakProcesses": [{"pid": p["pid"], "name": p["name"], "ussMiB": round(p["uss"] / MIB, 2)} for p in peak["processes"]],
        }
    for name in ("startup-idle", "static-idle", "dynamic-idle", "repeat-idle"):
        assert len(by_stage[name]) >= 10, f"空闲采样不足：{name}"
    for name in ("static-idle", "dynamic-idle", "repeat-idle"):
        assert not any(p["name"].lower() in ("python.exe", "chrome.exe", "node.exe")
                       for row in by_stage[name][-4:] for p in row["processes"]), f"采集子进程未回收：{name}"
    repeat_peak = max(stages[f"repeat-{i}"]["ussPeakMiB"] for i in range(1, 6))
    settled = [stages[f"repeat-{i}-settled"]["ussMedianMiB"] for i in range(1, 6)]
    baseline = stages["startup-idle"]["ussMedianMiB"]
    return {
        "variant": result["variant"], "sampleCount": len(rows), "excludedSamples": len(invalid),
        "excludedPercent": round(len(invalid)/len(raw_rows)*100,3), "intervalTargetMs": 250,
        "actualIntervalMedianMs": round(statistics.median(b["elapsed"]-a["elapsed"] for a,b in zip(rows, rows[1:]))*1000, 2),
        "maximumSampleCostMs": round(max(r["sampleSeconds"] for r in rows)*1000, 2),
        "startupIdleMiB": baseline,
        "startupMainMiB": stages["startup-idle"]["mainUssMedianMiB"],
        "staticPeakMiB": stages["static"]["ussPeakMiB"],
        "dynamicPairPeakMiB": stages["dynamic"]["ussPeakMiB"],
        "repeatPeakMiB": repeat_peak,
        "finalIdleMiB": stages["repeat-idle"]["ussMedianMiB"],
        "finalMainMiB": stages["repeat-idle"]["mainUssMedianMiB"],
        "overallPeakMiB": round(max(r["uss"] for r in rows) / MIB, 2),
        "finalIdleDeltaMiB": round(stages["repeat-idle"]["ussMedianMiB"]-baseline, 2),
        "repeatSettledMiB": settled, "repeatSettledGrowthMiB": round(settled[-1]-settled[0],2),
        "cooldowns": [e for e in result["events"] if "cooldown" in e],
        "batches": [e for e in result["events"] if "batch" in e],
        "contentHashes": [hashlib.sha256(c["content"].encode()).hexdigest() for c in result["captures"]],
        "stages": stages,
    }


def report(root):
    acceptance = read(root / "result.json")
    assert acceptance["passed"]
    phases = {name: phase_report(root / name) for name in acceptance["order"]}
    fields = ["startupIdleMiB", "staticPeakMiB", "dynamicPairPeakMiB", "repeatPeakMiB", "finalIdleMiB", "finalIdleDeltaMiB", "startupMainMiB", "finalMainMiB", "overallPeakMiB"]
    comparison = {}
    for field in fields:
        old = [p[field] for p in phases.values() if p["variant"] == "previous"]
        new = [p[field] for p in phases.values() if p["variant"] == "candidate"]
        old_mean, new_mean = statistics.mean(old), statistics.mean(new)
        comparison[field] = {"previous": old, "candidate": new,
                             "previousMean": round(old_mean,2), "candidateMean": round(new_mean,2),
                             "reductionPercent": round((old_mean-new_mean)/old_mean*100,1) if old_mean else None}
    reference = phases["previous-1"]["contentHashes"]
    equality = {name: sum(a == b for a,b in zip(reference, phase["contentHashes"])) for name,phase in phases.items()}
    return {"passed": True, "metric": "sum process-tree USS (private physical working set)",
            "comparison": comparison, "contentHashesEqualOutOf13": equality, "phases": phases}


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--run-root", type=Path, required=True)
    args = parser.parse_args()
    result = report(args.run_root)
    output = args.run_root / "memory-comparison.json"
    output.write_text(json.dumps(result, ensure_ascii=False, indent=2), encoding="utf-8")
    print(json.dumps(result["comparison"], indent=2))
    print(output)
