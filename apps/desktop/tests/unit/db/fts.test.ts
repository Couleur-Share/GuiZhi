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
