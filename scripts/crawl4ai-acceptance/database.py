"""只操作验收输出目录内、由已发布应用生成的合成数据库。"""
import argparse
import hashlib
import json
from pathlib import Path
import sqlite3
import time


def write(file, value):
    file.write_text(json.dumps(value, ensure_ascii=False, indent=2), encoding="utf-8")


def profile(root, phase):
    location = Path(json.loads((root / phase / "profile.json").read_text(encoding="utf-8"))["userDataDir"]).resolve()
    assert location.is_relative_to(root.resolve()), "拒绝操作验收范围外的数据"
    db = location / "data/knowledge.db"
    assert not Path(str(db) + ".lock").exists(), "测试数据库仍被占用"
    return db


def inspect(db):
    db.row_factory = sqlite3.Row
    return {
        "schemaVersion": db.execute("PRAGMA user_version").fetchone()[0],
        "migrations": [r[0] for r in db.execute("SELECT name FROM schema_migrations ORDER BY name")],
        "foreignKeyErrors": [dict(r) for r in db.execute("PRAGMA foreign_key_check")],
        "integrity": db.execute("PRAGMA integrity_check").fetchone()[0],
        "research": [dict(r) for r in db.execute("SELECT * FROM research_runs")],
        "sources": [dict(r) for r in db.execute("SELECT * FROM research_source_runs")],
        "candidates": [dict(r) for r in db.execute("SELECT * FROM research_candidates")],
        "indexes": [dict(r) for r in db.execute("SELECT name,tbl_name,sql FROM sqlite_master WHERE type='index' ORDER BY name")],
    }


def seed_old(root):
    file = profile(root, "previous-run")
    db = sqlite3.connect(file)
    now = int(time.time() * 1000)
    with db:
        db.execute("INSERT INTO research_runs(id,topic,day_range,range_from,range_to,depth,sources_json,status,report_status,report_markdown,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)", (
            "acceptance-old-research", "旧版研究证据", 30, now - 30*86400000, now, "quick", '["bilibili"]', "ready", "ready", "旧版报告及引用不可丢失。", now, now))
        db.execute("INSERT INTO research_source_runs(run_id,source,status,method,collected_count) VALUES (?,?,?,?,?)", ("acceptance-old-research", "bilibili", "succeeded", "acceptance-fixture", 1))
        db.execute("INSERT INTO research_candidates(id,run_id,source,external_id,url,normalized_url,title,media_type,discovery_method,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?)", (
            "acceptance-old-candidate", "acceptance-old-research", "bilibili", "BV-acceptance", "https://www.bilibili.com/video/BV-acceptance", "https://www.bilibili.com/video/BV-acceptance", "保留的旧版候选", "video", "acceptance-fixture", now, now))
    write(root / "previous-database.json", inspect(db))
    target = root / "previous-seeded.db"
    assert not target.exists(), "保留已有旧版种子"
    db.execute("VACUUM INTO ?", (str(target.resolve()),))
    db.close()
    write(root / "previous-seeded-hash.json", {"sha256": hashlib.sha256(target.read_bytes()).hexdigest()})


def verify_upgrade(root, phase):
    before = json.loads((root / "previous-database.json").read_text(encoding="utf-8"))
    db = sqlite3.connect(profile(root, phase))
    after = inspect(db)
    assert after["integrity"] == "ok" and not after["foreignKeyErrors"]
    assert after["schemaVersion"] == 30
    assert after["sources"] == before["sources"]
    assert after["candidates"] == before["candidates"]
    assert [{k: v for k, v in row.items() if k != "time_scope"} for row in after["research"]] == before["research"]
    assert all(r["time_scope"] == "recent" for r in after["research"])
    old_indexes = {r["name"] for r in before["indexes"]}
    assert old_indexes <= {r["name"] for r in after["indexes"]}
    backup_dir = profile(root, phase).parent.parent / "backups"
    backups = list(backup_dir.glob("knowledge-pre-update-*.db"))
    assert len(backups) == 1, "升级前必须产生一次完整快照"
    with sqlite3.connect(backups[0]) as backup:
        saved = inspect(backup)
        assert saved["schemaVersion"] == before["schemaVersion"]
        assert saved["candidates"] == before["candidates"]
    write(root / phase / "database-check.json", {"passed": True, "beforeSchema": before["schemaVersion"], "afterSchema": after["schemaVersion"], "oldIndexesPreserved": len(old_indexes), "backup": str(backups[0])})
    db.close()


def verify_restored(root, phase):
    capture = json.loads((root / "capture-final/capture.json").read_text(encoding="utf-8"))
    snapshot = Path(capture["backup"]["backup"]["path"]).resolve()
    assert snapshot.is_relative_to(root.resolve())
    with sqlite3.connect(snapshot) as before, sqlite3.connect(profile(root, phase)) as after:
        assert after.execute("PRAGMA integrity_check").fetchone()[0] == "ok"
        assert not after.execute("PRAGMA foreign_key_check").fetchall()
        tables = [row[0] for row in before.execute("SELECT name FROM sqlite_master WHERE type='table' AND (name IN ('knowledge_items','collections','tags','knowledge_item_tags','source_records','web_source_versions','web_source_baselines') OR name LIKE 'research_%')")]
        for table in tables:
            assert sorted(before.execute(f'SELECT * FROM "{table}"').fetchall(), key=repr) == sorted(after.execute(f'SELECT * FROM "{table}"').fetchall(), key=repr), table
        assert after.execute("SELECT status FROM crawl_jobs WHERE id=?", (capture["interruptedJobId"],)).fetchone()[0] == "interrupted"
        assert after.execute("SELECT status FROM crawl_pages WHERE job_id=?", (capture["interruptedJobId"],)).fetchone()[0] == "pending"
        write(root / phase / "database-check.json", {"passed": True, "exactlyPreservedTables": tables, "integrity": "ok", "foreignKeyErrors": [], "interruptedQueue": "awaiting-user"})


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("command", choices=["seed-old", "verify-upgrade", "verify-restored"])
    parser.add_argument("--root", type=Path, required=True)
    parser.add_argument("--phase", default="upgrade-run")
    args = parser.parse_args()
    if args.command == "seed-old": seed_old(args.root)
    elif args.command == "verify-upgrade": verify_upgrade(args.root, args.phase)
    else: verify_restored(args.root, args.phase)
