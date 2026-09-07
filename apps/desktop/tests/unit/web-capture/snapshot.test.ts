import { describe, expect, it, afterEach } from "vitest";
import { MIGRATIONS } from "@guizhi/db/migrations";
import Database from "@guizhi/db/adapter";
import { SCHEMA_TABLES, SCHEMA_INDEXES } from "@guizhi/db/schema";
import { KnowledgeItemDB, WebSourceDB, webContentHash } from "@guizhi/db";
import { resolveSourcePlatform } from "@guizhi/shared/utils/source-platforms";
import type { WebCaptureResult, WebSnapshot } from "@guizhi/shared/types";
import {
  cleanCss,
  sanitizeSnapshot,
  snapshotHash,
} from "../../../src/main/services/web-capture/snapshot-sanitize";
import { snapshotDocument } from "../../../src/main/services/web-capture/snapshot-document";
import { SNAPSHOT_BRIDGE_HASH } from "../../../src/main/services/web-capture/snapshot-bridge";

const fileName = `wechat-${"a".repeat(64)}.png`,
  local = `local-image://${fileName}`;
function snapshot(
  html = '<section style="border:1px solid #987;text-align:center"><p>一天可以游玩完黄山吗？</p><span style="border-radius:50%;border:1px solid gray">1</span><h2>关于黄山</h2></section>',
): WebSnapshot {
  const value: WebSnapshot = {
    formatVersion: 1,
    policyVersion: 1,
    adapterVersion: "wechat-html/1",
    html,
    css: "",
    hash: "",
    account: "测试公众号",
    author: "作者",
    publishedAt: null,
    assets: [
      {
        fileName,
        sourceUrl: "https://mmbiz.qpic.cn/a.png",
        sha256: "a".repeat(64),
        bytes: 10,
      },
    ],
    failures: [],
    warnings: [],
  };
  value.hash = snapshotHash(value);
  return value;
}
const dbs: Database[] = [];
afterEach(() => dbs.splice(0).forEach((db) => db.close()));
function fixture() {
  const db = new Database(":memory:");
  dbs.push(db);
  db.pragma("foreign_keys=ON");
  db.exec(SCHEMA_TABLES);
  db.exec(SCHEMA_INDEXES);
  return db;
}
function result(value = snapshot()): WebCaptureResult {
  return {
    taskId: "test",
    entryUrl: "https://mp.weixin.qq.com/s/a",
    finalUrl: "https://mp.weixin.qq.com/s/a",
    title: "原文",
    author: "作者",
    publishedAt: null,
    dateConfidence: "unknown",
    markdown: `正文 ![](${local})`,
    links: [],
    paragraphs: [],
    contentHash: "",
    capturedAt: Date.now(),
    engineVersion: "wechat-html/1",
    complete: true,
    truncated: false,
    warnings: [],
    snapshot: value,
  };
}
describe("公众号 HTML 快照", () => {
  it("旧库回填只改公众号平台，保留正文、编辑状态和其他平台",()=>{
    const db=fixture(), items=new KnowledgeItemDB(db), item=items.create({title:"旧文章",content:"人工编辑"});
    db.run("INSERT INTO source_records(id,item_id,source_type,source_uri,platform,captured_at) VALUES ('wechat-old',?,'url','https://mp.weixin.qq.com/s/a','web',1)",item.id);
    db.run("INSERT INTO source_records(id,item_id,source_type,source_uri,platform,captured_at) VALUES ('other',?,'url','https://example.com/a','web',1)",item.id);
    const before=items.get(item.id);
    MIGRATIONS.find(m=>m.name==="0031-wechat-snapshots").up(db);
    expect(items.get(item.id)).toEqual(before);
    expect(db.get("SELECT platform FROM source_records WHERE id='wechat-old'")).toEqual({platform:"wechat"});
    expect(db.get("SELECT platform FROM source_records WHERE id='other'")).toEqual({platform:"web"});
  });
  it("统一网页入口的公众号来源更新也必须待比较",()=>{
    const db=fixture(),items=new KnowledgeItemDB(db),sources=new WebSourceDB(db),first=result();
    const item=items.create({title:first.title,content:first.markdown});sources.initialize(item.id,first);
    expect(sources.check(item.id,result(snapshot('<p style="color:red">排版变更</p>')))).toBe("pending-version");
    expect(items.get(item.id).content).toBe(first.markdown);
  });

  it("严格平台识别与伪造域名", () => {
    expect(resolveSourcePlatform("url", "https://mp.weixin.qq.com/s/a")).toBe(
      "wechat",
    );
    expect(
      resolveSourcePlatform("url", "https://mp.weixin.qq.com.evil.test/s/a"),
    ).toBe("web");
    expect(
      resolveSourcePlatform("url", "https://evil.test/?next=mp.weixin.qq.com"),
    ).toBe("web");
  });
  it("保留问题框、编号圆角和居中，清理主动内容及远程样式资源", () => {
    const original = snapshot();
    const clean = sanitizeSnapshot({
      ...original,
      html:
        original.html +
        `<script>alert(1)</script><img src="https://evil.test/a" onerror="alert(1)"><iframe src="https://evil.test"></iframe><svg onload="alert(1)"><circle r="3" fill="url(https://evil.test)"/><foreignObject><div>恶意</div></foreignObject></svg>`,
    });
    expect(clean.html).toContain("text-align:center");
    expect(clean.html).toContain("border-radius:50%");
    expect(clean.html).not.toMatch(
      /script|onerror|onload|iframe|foreignobject|https:\/\/evil/i,
    );
    const css = cleanCss(
      "@import url(https://evil.test);.a{position:fixed;background:url(https://evil.test);color:red}.b::before{content:'1';border:1px solid #987}",
      () => undefined,
      true,
    );
    expect(css).toContain("color:red");
    expect(css).toContain("::before");
    expect(css).not.toMatch(/import|fixed|evil/);
    expect(
      cleanCss(
        "background:u\\72l(https://evil.test);width:expression(alert(1));height:var(--x)",
        () => undefined,
      ),
    ).not.toMatch(/evil|expression|var/);
  });
  it("损坏资源清单与越界路径被拒绝，固定脚本通过哈希限定", () => {
    expect(() =>
      sanitizeSnapshot({
        ...snapshot(),
        assets: [{ ...snapshot().assets[0], fileName: "../../secret.png" }],
      }),
    ).toThrow();
    const doc = snapshotDocument(snapshot(), "test-instance");
    expect(doc).toContain(`sha256-${SNAPSHOT_BRIDGE_HASH}`);
    expect(doc).toContain("connect-src &#39;none&#39;");
    expect(snapshotDocument(snapshot())).not.toContain("<script>");
  });
  it("仅样式变化生成待比较版本；补采与并发采用不覆盖编辑", () => {
    const db = fixture(),
      items = new KnowledgeItemDB(db),
      sources = new WebSourceDB(db),
      first = result();
    const item = items.create({
      title: "原文",
      content: first.markdown,
      itemType: "webpage",
    });
    sources.initialize(item.id, first);
    items.update(item.id, { content: "我编辑的正文" });
    sources.attach(item.id, result(snapshot('<p style="color:blue">正文</p>')));
    expect(items.get(item.id).content).toBe("我编辑的正文");
    expect(sources.versions(item.id)).toHaveLength(2);
    const latest = sources.versions(item.id)[0];
    expect(() =>
      sources.adopt({
        itemId: item.id,
        versionId: latest.id,
        expectedContentHash: webContentHash("过时正文"),
        expectedTitle: "原文",
      }),
    ).toThrow("变化");
    sources.adopt({
      itemId: item.id,
      versionId: latest.id,
      expectedContentHash: webContentHash("我编辑的正文"),
      expectedTitle: "原文",
    });
    expect(
      sources
        .versions(item.id)
        .some((v) => v.kind === "local" && v.markdown === "我编辑的正文"),
    ).toBe(true);
  });
  it("历史版本和回收站持有的资源不得当作孤儿删除", () => {
    const db = fixture(),
      items = new KnowledgeItemDB(db),
      sources = new WebSourceDB(db);
    const item = items.create({
      title: "原文",
      content: "纯文本",
      itemType: "webpage",
    });
    sources.initialize(item.id, result());
    expect(items.listReferencedAssets().has(fileName)).toBe(true);
    expect(items.listAssetRefs([item.id])).toContain(fileName);
    items.moveToTrash([item.id]);
    expect(items.listReferencedAssets().has(fileName)).toBe(true);
    items.deleteForever([item.id]);
    expect(items.listReferencedAssets().has(fileName)).toBe(false);
    expect(db.all("SELECT * FROM web_snapshot_assets")).toEqual([]);
  });
});
