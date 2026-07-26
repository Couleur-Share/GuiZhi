import { describe, expect, it } from "vitest";
import { buildFtsMatchQuery, segmentTextForFts } from "@guizhi/db/fts";

describe("segmentTextForFts", () => {
  it("中文逐字分隔", () => {
    expect(segmentTextForFts("归知")).toBe("归 知");
  });

  it("中英混排保留英文单词", () => {
    expect(segmentTextForFts("归知App测试")).toBe("归 知 App 测 试");
  });

  it("空串返回空", () => {
    expect(segmentTextForFts("")).toBe("");
  });

  it("多余空白被规整", () => {
    expect(segmentTextForFts("hello   world")).toBe("hello world");
  });
});

describe("buildFtsMatchQuery", () => {
  it("中文串生成按字 phrase", () => {
    expect(buildFtsMatchQuery("知识库")).toBe('"知 识 库"');
  });

  it("英文词生成前缀匹配", () => {
    expect(buildFtsMatchQuery("elec")).toBe('"elec"*');
  });

  it("混合查询以 AND 连接", () => {
    expect(buildFtsMatchQuery("知识 App")).toBe('"知 识" AND "App"*');
  });

  it("空查询返回 null", () => {
    expect(buildFtsMatchQuery("   ")).toBeNull();
  });

  it("双引号被转义", () => {
    expect(buildFtsMatchQuery('a"b')).toBe('"a""b"*');
  });

  it("纯标点片段被丢弃，不产生匹配 0 行的空 phrase", () => {
    // 标点分词后是空 phrase，与其他子句 AND 会把整个查询清零
    expect(buildFtsMatchQuery("归知(测试)")).toBe('"归 知" AND "测 试"');
    expect(buildFtsMatchQuery("第一章：概述")).toBe('"第 一 章" AND "概 述"');
    expect(buildFtsMatchQuery("React —— 入门")).toBe(
      '"React"* AND "入 门"',
    );
  });

  it("含字母数字的片段照常保留", () => {
    expect(buildFtsMatchQuery(".NET")).toBe('".NET"*');
    expect(buildFtsMatchQuery("C++")).toBe('"C++"*');
  });

  it("全标点查询视为无可检索内容", () => {
    expect(buildFtsMatchQuery("???")).toBeNull();
    expect(buildFtsMatchQuery("——")).toBeNull();
  });
});

describe("buildFtsMatchQuery 召回模式", () => {
  it("按虚词切开长句并以 OR 连接", () => {
    // phrase 模式下整句会变成一个逐字相邻的长 phrase，必然零命中
    expect(buildFtsMatchQuery("归知的语义检索是怎么实现的")).toBe(
      '"归 知 的 语 义 检 索 是 怎 么 实 现 的"',
    );
    expect(buildFtsMatchQuery("归知的语义检索是怎么实现的", "recall")).toBe(
      '"归 知" OR "语 义 检 索" OR "实 现"',
    );
  });

  it("疑问词整体剥离，不被短虚词切碎", () => {
    expect(buildFtsMatchQuery("为什么采集会失败", "recall")).toBe(
      '"采 集" OR "失 败"',
    );
  });

  it("中英混排：英文仍走前缀匹配", () => {
    expect(buildFtsMatchQuery("Electron 的窗口安全", "recall")).toBe(
      '"Electron"* OR "窗 口 安 全"',
    );
  });

  it("整句都是虚词时退回原串，不返回 null", () => {
    // 返回 null 会被调用方当成「没有搜索」进而列出全库
    expect(buildFtsMatchQuery("这个是什么", "recall")).toBe(
      '"这 个 是 什 么"',
    );
  });

  it("子句数量有上限", () => {
    const long = "采集 转写 摘要 标签 问答 图谱 备份 导出 索引 迁移";
    const clauses = buildFtsMatchQuery(long, "recall")!.split(" OR ");
    expect(clauses.length).toBeLessThanOrEqual(8);
  });

  it("空查询两种模式都返回 null", () => {
    expect(buildFtsMatchQuery("   ", "recall")).toBeNull();
    expect(buildFtsMatchQuery("——", "recall")).toBeNull();
  });
});
