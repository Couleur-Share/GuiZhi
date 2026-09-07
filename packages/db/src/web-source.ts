import { createHash, randomUUID } from "node:crypto";
import type Database from "./adapter";
import { KnowledgeItemDB } from "./knowledge";
import type {
  WebCaptureResult,
  WebSourceVersion,
  AdoptWebVersionInput,
} from "@guizhi/shared/types";

export const webContentHash = (text: string): string =>
  createHash("sha256").update(text).digest("hex");
interface Baseline {
  content_hash: string;
  title: string;
  remote_hash: string;
  version_id: string;
  summary_stale: number;
}
export class WebSourceDB {
  constructor(private db: Database) {}
  versions(itemId: string): WebSourceVersion[] {
    return (
      this.db.all(
        "SELECT payload FROM web_source_versions WHERE item_id=? ORDER BY captured_at DESC,rowid DESC",
        itemId,
      ) as { payload: string }[]
    ).map((r) => JSON.parse(r.payload));
  }
  baseline(itemId: string): Baseline | null {
    return (
      (this.db.get(
        "SELECT * FROM web_source_baselines WHERE item_id=?",
        itemId,
      ) as Baseline) ?? null
    );
  }
  private save(version: WebSourceVersion): void {
    this.db.run(
      "INSERT INTO web_source_versions VALUES (?,?,?,?)",
      version.id,
      version.itemId,
      JSON.stringify(version),
      version.capturedAt,
    );
    for (const asset of version.snapshot?.assets ?? []) {
      if (!/^wechat-[a-f0-9]{64}\.(png|jpg|gif|webp)$/.test(asset.fileName)) throw new Error("快照资源名称无效");
      this.db.run("INSERT OR IGNORE INTO web_snapshot_assets VALUES (?,?)", version.id, asset.fileName);
    }
  }
  /** 补采仅添加来源版本，不覆盖用户正文。 */
  attach(itemId: string, result: WebCaptureResult): void {
    const item = new KnowledgeItemDB(this.db).get(itemId);
    if (!item || item.deletedAt) throw new Error("条目不存在或已删除");
    const latest = this.versions(itemId).find(v => v.kind === "remote");
    if (latest?.snapshot?.hash === result.snapshot?.hash && latest?.contentHash === webContentHash(result.markdown) && latest?.title === result.title) return;
    const version = this.remote(itemId, result);
    this.db.transaction(() => {
      this.save(version);
      if (!this.baseline(itemId)) this.setBaseline(itemId, version, item.content, item.title, false);
    })();
  }
  private remote(itemId: string, result: WebCaptureResult): WebSourceVersion {
    return {
      id: randomUUID(),
      itemId,
      sourceUrl: result.finalUrl,
      title: result.title,
      markdown: result.markdown,
      contentHash: webContentHash(result.markdown),
      capturedAt: result.capturedAt,
      engineVersion: result.engineVersion,
      complete: result.complete && !result.truncated && !result.error,
      kind: "remote",
      snapshot: result.snapshot,
    };
  }
  private setBaseline(
    itemId: string,
    version: WebSourceVersion,
    content: string,
    title: string,
    stale: boolean,
  ): void {
    this.db.run(
      "INSERT INTO web_source_baselines VALUES (?,?,?,?,?,?,?) ON CONFLICT(item_id) DO UPDATE SET version_id=excluded.version_id,content_hash=excluded.content_hash,title=excluded.title,checked_at=excluded.checked_at,remote_hash=excluded.remote_hash,summary_stale=excluded.summary_stale",
      itemId,
      version.id,
      webContentHash(content),
      title,
      Date.now(),
      version.contentHash,
      Number(stale),
    );
  }
  initialize(itemId: string, result: WebCaptureResult): void {
    const item = new KnowledgeItemDB(this.db).get(itemId);
    if (!item || this.baseline(itemId)) return;
    const version = this.remote(itemId, result);
    this.db.transaction(() => {
      this.save(version);
      this.setBaseline(itemId, version, item.content, item.title, false);
    })();
  }
  check(
    itemId: string,
    result: WebCaptureResult,
  ): "updated" | "unchanged" | "pending-version" {
    if (!result.complete || result.truncated || result.error)
      throw new Error("不完整或失败正文不能更新已有条目");
    return this.db.transaction(() => {
      const items = new KnowledgeItemDB(this.db),
        item = items.get(itemId);
      if (!item || item.deletedAt) throw new Error("条目不存在或已删除");
      const baseline = this.baseline(itemId),
        version = this.remote(itemId, result);
      const previousRemote = this.versions(itemId).find(
        (v) => v.kind === "remote",
      );
      if (
        previousRemote?.contentHash === version.contentHash &&
        previousRemote.title === version.title &&
        previousRemote.snapshot?.hash === version.snapshot?.hash
      ) {
        this.db.run(
          "UPDATE web_source_baselines SET checked_at=? WHERE item_id=?",
          Date.now(),
          itemId,
        );
        return baseline?.version_id === previousRemote.id
          ? ("unchanged" as const)
          : ("pending-version" as const);
      }
      this.save(version);
      if (version.snapshot || !baseline || webContentHash(item.content) !== baseline.content_hash)
        return "pending-version" as const;
      this.snapshot(itemId, item.title, item.content, version.sourceUrl);
      const title = item.title === baseline.title ? version.title : item.title;
      items.update(itemId, { content: version.markdown, title });
      this.invalidate(itemId);
      this.setBaseline(
        itemId,
        version,
        version.markdown,
        item.title === baseline.title ? title : baseline.title,
        !!item.summary,
      );
      return "updated" as const;
    })();
  }
  private snapshot(
    itemId: string,
    title: string,
    markdown: string,
    sourceUrl: string,
  ): void {
    this.save({
      id: randomUUID(),
      itemId,
      title,
      markdown,
      sourceUrl,
      contentHash: webContentHash(markdown),
      capturedAt: Date.now(),
      engineVersion: "guizhi",
      complete: true,
      kind: "local",
    });
  }
  private invalidate(itemId: string): void {
    this.db.run("DELETE FROM knowledge_embeddings WHERE item_id=?", itemId);
    this.db.run("DELETE FROM wiki_ingestions WHERE item_id=?", itemId);
  }
  adopt(input: AdoptWebVersionInput): void {
    this.db.transaction(() => {
      const items = new KnowledgeItemDB(this.db),
        item = items.get(input.itemId);
      const version = this.versions(input.itemId).find(
        (v) => v.id === input.versionId,
      );
      if (!item || item.deletedAt || !version?.complete)
        throw new Error("条目或完整版本不存在");
      if (
        webContentHash(item.content) !== input.expectedContentHash ||
        item.title !== input.expectedTitle
      )
        throw new Error("正文或标题已在比较期间发生变化，请重新比较后采用");
      this.snapshot(item.id, item.title, item.content, version.sourceUrl);
      const baseline = this.baseline(item.id);
      const title =
        baseline && item.title === baseline.title ? version.title : item.title;
      items.update(item.id, { content: version.markdown, title });
      this.invalidate(item.id);
      this.setBaseline(
        item.id,
        version,
        version.markdown,
        baseline?.title === item.title
          ? title
          : (baseline?.title ?? "__legacy_unknown_title__"),
        !!item.summary,
      );
    })();
  }
}
