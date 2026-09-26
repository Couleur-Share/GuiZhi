"""只读核对沙盒回传的合成数据库、正文版本与缓存；不连接用户知识库。"""
import argparse
import hashlib
import json
from pathlib import Path, PureWindowsPath
import sqlite3


def read(file):
    return json.loads(file.read_text(encoding="utf-8-sig"))


def audit(root):
    root = root.resolve()
    result = read(root / "result.json")
    assert result["passed"] and result["source"] == "windows-sandbox"
    old = read(root / "previous/installed.json")
    manual_id = old["manual"]["id"]
    old_version_ids = [r["id"] for r in old["captures"][0]["versions"]]
    databases = {}
    previous_row = None
    previous_versions = None
    for phase in ("previous", "upgrade", "clean"):
        relative = PureWindowsPath(read(root / phase / "profile.json")["userDataDir"]).relative_to(
            PureWindowsPath("C:/GuiZhiAcceptanceOutput") / root.name)
        profile = root.joinpath(*relative.parts).resolve()
        assert profile.is_relative_to(root), "验收数据越界"
        db_path = profile / "data/knowledge.db"
        assert not Path(str(db_path) + ".lock").exists(), "数据库仍被占用"
        before_hash = hashlib.sha256(db_path.read_bytes()).hexdigest()
        with sqlite3.connect(db_path.as_uri() + "?mode=ro", uri=True) as db:
            db.row_factory = sqlite3.Row
            assert db.execute("PRAGMA integrity_check").fetchone()[0] == "ok"
            assert not db.execute("PRAGMA foreign_key_check").fetchall()
            row = db.execute("SELECT * FROM knowledge_items WHERE id=?", (manual_id,)).fetchone()
            versions = [dict(db.execute("SELECT * FROM web_source_versions WHERE id=?", (version_id,)).fetchone())
                        for version_id in old_version_ids] if phase != "clean" else []
            if phase == "previous":
                previous_row, previous_versions = dict(row), versions
            elif phase == "upgrade":
                assert dict(row) == previous_row, "人工条目数据库字段发生变化"
                assert versions == previous_versions, "原网页版本字段发生变化"
            else:
                assert row is None, "全新数据库混入旧测试条目"
            databases[phase] = {"integrity": "ok", "foreignKeyErrors": [],
                                "schemaVersion": db.execute("PRAGMA user_version").fetchone()[0],
                                "knowledgeCount": db.execute("SELECT COUNT(*) FROM knowledge_items").fetchone()[0],
                                "sha256": before_hash}
        assert hashlib.sha256(db_path.read_bytes()).hexdigest() == before_hash, "只读审计改变了数据库"
        assert not list((profile / "cache/web-capture").glob("owned-*")), "采集缓存没有回收"
    return {"passed": True, "readOnlyAudit": True, "manualRowPreserved": True,
            "oldWebVersionRowsPreserved": True, "cleanProfileIndependent": True,
            "captureCacheEmpty": True, "databases": databases}


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--run-root", type=Path, required=True)
    args = parser.parse_args()
    report = audit(args.run_root)
    output = args.run_root / "host-audit.json"
    output.write_text(json.dumps(report, indent=2), encoding="utf-8")
    print(output)
