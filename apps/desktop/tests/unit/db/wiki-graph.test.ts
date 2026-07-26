import { beforeEach, describe, expect, it } from "vitest";
import DatabaseAdapter from "@guizhi/db/adapter";
import { SCHEMA_INDEXES, SCHEMA_TABLES } from "@guizhi/db/schema";
import { KnowledgeItemDB } from "@guizhi/db/knowledge";
import { WikiDB } from "@guizhi/db/wiki";

function createTestDb(): DatabaseAdapter.Database {
  const db = new DatabaseAdapter(":memory:");
  db.pragma("foreign_keys = ON");
  db.exec(SCHEMA_TABLES);
  db.exec(SCHEMA_INDEXES);
  return db;
}

describe("WikiDB.getGraph", () => {
  let db: DatabaseAdapter.Database;
  let wiki: WikiDB;

  beforeEach(() => {
    db = createTestDb();
    wiki = new WikiDB(db);
  });

  it("返回页面节点与物化的页间链接", () => {
    const item = new KnowledgeItemDB(db).create({
      title: "素材",
      content: "内容",
    });
    wiki.applyCompilation({
      itemId: item.id,
      contentHash: "hash-1",
      provider: "test",
      model: "test-model",
      promptVersion: "v1",
      pages: [
        {
          title: "页面A",
          normalizedTitle: "页面a",
          kind: "topic",
          summary: "A 的摘要",
          body: "指向 [[页面B]] 的正文",
          aliasesJson: null,
          linkTargets: ["页面b"],
        },
        {
          title: "页面B",
          normalizedTitle: "页面b",
          kind: "entity",
          summary: "B 的摘要",
          body: "无出链",
          aliasesJson: null,
          linkTargets: [],
        },
      ],
    });

    const graph = wiki.getGraph();
    expect(graph.nodes).toHaveLength(2);
    expect(graph.nodes.map((node) => node.title).sort()).toEqual([
      "页面A",
      "页面B",
    ]);
    expect(graph.nodes.find((node) => node.title === "页面A")?.kind).toBe(
      "topic",
    );

    expect(graph.links).toHaveLength(1);
    const idByTitle = new Map(
      graph.nodes.map((node) => [node.title, node.id]),
    );
    expect(graph.links[0]).toEqual({
      source: idByTitle.get("页面A"),
      target: idByTitle.get("页面B"),
    });
  });

  it("空库返回空图", () => {
    expect(wiki.getGraph()).toEqual({ nodes: [], links: [], totalNodes: 0 });
  });
});
