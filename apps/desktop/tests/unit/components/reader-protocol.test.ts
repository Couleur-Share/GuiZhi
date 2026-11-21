import { describe, expect, it } from "vitest";
import { parseHostMessage } from "../../../src/renderer/reader/protocol";
import { rewriteMediaUrl } from "../../../src/renderer/reader/media-urls";

const theme = {
  dark: true,
  hue: 210,
  saturation: 35,
  basePx: 16,
  motion: "standard",
};

describe("阅读岛协议：宿主消息的解析", () => {
  it("init / theme 带着外观与语言", () => {
    expect(
      parseHostMessage(JSON.stringify({ t: "init", v: 1, theme, lang: "en" })),
    ).toEqual({ t: "init", v: 1, theme, lang: "en" });
    expect(
      parseHostMessage(JSON.stringify({ t: "theme", theme, lang: "zh" })),
    ).toEqual({ t: "theme", theme, lang: "zh" });
  });

  it("语言只认 en，其余一律按中文", () => {
    const parsed = parseHostMessage(
      JSON.stringify({ t: "theme", theme, lang: "fr" }),
    );
    expect(parsed).toMatchObject({ t: "theme", lang: "zh" });
  });

  it("外观字段缺失、类型不对、根字号越界都丢弃", () => {
    const bad = [
      { ...theme, dark: "yes" },
      { ...theme, hue: "210" },
      { ...theme, motion: "fast" },
      { ...theme, basePx: 0 },
      { ...theme, basePx: 41 },
      { ...theme, hue: null },
    ];
    for (const value of bad) {
      expect(
        parseHostMessage(JSON.stringify({ t: "theme", theme: value, lang: "zh" })),
      ).toBeNull();
    }
  });

  it("view 校验标签与分支，restoreTop 取非负有限数", () => {
    const base = {
      t: "view",
      seq: 3,
      itemId: "item-1",
      tab: "body",
      view: { kind: "original", content: "正文", sourceKind: "body" },
      restoreTop: -5,
      catalogOpen: true,
    };
    expect(parseHostMessage(JSON.stringify(base))).toEqual({
      t: "view",
      seq: 3,
      itemId: "item-1",
      tab: "body",
      view: { kind: "original", content: "正文", sourceKind: "body" },
      restoreTop: 0,
      catalogOpen: true,
      tocOpen: false,
    });
    expect(
      parseHostMessage(JSON.stringify({ ...base, restoreTop: "12" })),
    ).toMatchObject({ restoreTop: null });
    expect(
      parseHostMessage(JSON.stringify({ ...base, tab: "nope" })),
    ).toBeNull();
    expect(
      parseHostMessage(JSON.stringify({ ...base, view: { kind: "original", content: 1 } })),
    ).toBeNull();
    expect(
      parseHostMessage(
        JSON.stringify({ ...base, view: { kind: "markdown", content: "x", tab: "bogus" } }),
      ),
    ).toBeNull();
    expect(
      parseHostMessage(JSON.stringify({ ...base, view: { kind: "mystery" } })),
    ).toBeNull();
    expect(parseHostMessage(JSON.stringify({ ...base, seq: "3" }))).toBeNull();
  });

  it("replies / gallery / none 三种分支", () => {
    const base = { t: "view", seq: 1, itemId: "a", tab: "replies", restoreTop: null };
    expect(
      parseHostMessage(JSON.stringify({ ...base, view: { kind: "replies", content: "c" } })),
    ).toMatchObject({ view: { kind: "replies", content: "c" } });
    expect(
      parseHostMessage(JSON.stringify({ ...base, view: { kind: "gallery", content: "g" } })),
    ).toMatchObject({ view: { kind: "gallery", content: "g" } });
    expect(
      parseHostMessage(JSON.stringify({ ...base, view: { kind: "none" } })),
    ).toMatchObject({ view: { kind: "none" } });
  });

  it("find 的序号取整，非数字丢弃；未知类型与坏 JSON 丢弃", () => {
    expect(
      parseHostMessage(JSON.stringify({ t: "find", query: "词", index: 2.9 })),
    ).toEqual({ t: "find", query: "词", index: 2 });
    expect(parseHostMessage(JSON.stringify({ t: "find", query: 1, index: 0 }))).toBeNull();
    expect(parseHostMessage(JSON.stringify({ t: "find", query: "a" }))).toBeNull();
    expect(parseHostMessage(JSON.stringify({ t: "unknown" }))).toBeNull();
    expect(parseHostMessage("{not json")).toBeNull();
    expect(parseHostMessage("[]")).toBeNull();
    expect(parseHostMessage("null")).toBeNull();
  });

  it("ui 只认严格的 true", () => {
    expect(
      parseHostMessage(JSON.stringify({ t: "ui", catalogOpen: 1, tocOpen: true })),
    ).toEqual({ t: "ui", catalogOpen: false, tocOpen: true });
  });

  it("copyResult 要整数 id 与布尔 ok，其余丢弃", () => {
    expect(parseHostMessage(JSON.stringify({ t: "copyResult", id: 3, ok: true }))).toEqual({
      t: "copyResult",
      id: 3,
      ok: true,
    });
    expect(parseHostMessage(JSON.stringify({ t: "copyResult", id: 3, ok: false }))).toEqual({
      t: "copyResult",
      id: 3,
      ok: false,
    });
    for (const bad of [
      { t: "copyResult", id: 3 },
      { t: "copyResult", id: 3, ok: "true" },
      { t: "copyResult", id: 1.5, ok: true },
      { t: "copyResult", id: "3", ok: true },
      { t: "copyResult", ok: true },
      { t: "copyResult", id: 1e300, ok: true },
    ]) {
      expect(parseHostMessage(JSON.stringify(bad))).toBeNull();
    }
  });
});

describe("阅读岛里的本地媒体地址改写", () => {
  it("local-image / local-video 改成岛自己的 /media 路径", () => {
    expect(rewriteMediaUrl("local-image://abc-1.png")).toBe(
      "./media/local-image/abc-1.png",
    );
    expect(rewriteMediaUrl("local-video://clip.mp4")).toBe(
      "./media/local-video/clip.mp4",
    );
  });

  it("名称按解码再编码规整：中文与已编码写法得到同一个结果", () => {
    expect(rewriteMediaUrl("local-image://图 1.png")).toBe(
      "./media/local-image/%E5%9B%BE%201.png",
    );
    expect(rewriteMediaUrl("local-image://%E5%9B%BE%201.png")).toBe(
      "./media/local-image/%E5%9B%BE%201.png",
    );
  });

  it("多余的分隔符被折叠，非法百分号序列不抛错", () => {
    expect(rewriteMediaUrl("local-image://a//b\\c.png")).toBe(
      "./media/local-image/a/b/c.png",
    );
    expect(rewriteMediaUrl("local-image://100%.png")).toBe(
      "./media/local-image/100%25.png",
    );
  });

  it("不是本地媒体地址的原样返回", () => {
    for (const value of [
      "https://example.com/a.png",
      "data:image/png;base64,AAAA",
      "./media/local-image/x.png",
      "",
      "local-image:/x.png",
    ]) {
      expect(rewriteMediaUrl(value)).toBe(value);
    }
  });
});
