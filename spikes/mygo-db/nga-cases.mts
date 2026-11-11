// NGA 连接器差分夹具：真实 nga.ts（经 esbuild 打包）跑在 BBCode、清洗函数与脚本化的整帖抓取上。
// 由 golden-forum.mts 动态引入；记录的 kind 均以 "nga" 开头，Go 端由 forum_nga_golden_test.go 对拍。
import crypto from "node:crypto";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

/* eslint-disable @typescript-eslint/no-explicit-any */
type Add = (kind: string, name: string, input: unknown, output: unknown) => void;
type Outcome = { status?: number; text?: string; setCookies?: string[]; error?: string };
type Fixture = Record<string, Outcome | Outcome[]>;

const NGA = "https://bbs.nga.cn";
const pg = (tid: string, page: number, authorid?: number) => `${NGA}/read.php?tid=${tid}&page=${page}&lite=js${authorid ? `&authorid=${authorid}` : ""}`;
const pidU = (pid: number) => `${NGA}/read.php?pid=${pid}&lite=js`;
const store = (data: unknown, error?: unknown): Outcome => ({ text: "window.script_muti_get_var_store=" + JSON.stringify({ data, error, encode: "gbk" }) + ";" });
const PNG = "iVBORw0KGgo=";

const post = (lou: number, authorid: number | undefined, content: string, extra: Record<string, unknown> = {}) => ({
  lou, authorid, pid: 9000 + lou * 7 + (authorid ?? 0), content, postdatetimestamp: 1700000000 + lou * 60, ...extra,
});
const users = { "11": { username: " 楼主 " }, "22": { username: "路人甲" }, "33": { username: "" } };
const topic = (extra: Record<string, unknown> = {}) => ({ tid: 100, fid: 7, subject: "标题 &amp;amp; [测试]", authorid: 11, replies: 44, postdate: 1700000000, ...extra });
const pageData = (over: Record<string, unknown>) => ({
  __U: users, __F: { "7": { name: " 版块A " } }, __GLOBAL: { _ATTACH_BASE_VIEW: "img.nga.cn/attachments" }, ...over,
});

interface ThreadCase {
  name: string;
  tid: string;
  fixture: Fixture;
  images?: Record<string, string>; // url → base64（缺省即下载 404）
  retryDelays?: number[];
  imageLimit?: number;
  maxSummaryPages?: number;
  maxOpPages?: number;
  maxParentFetches?: number;
}

export async function addNgaCases(T: any, add: Add, norm: (v: unknown) => unknown) {
  // ---------- 纯函数 ----------
  const bbcodes: { raw: string; attachBase?: string; imageMap?: Record<string, string> }[] = [
    { raw: "普通文本&amp;&lt;b&gt; <br/>换行\r\n第二行&#39;&quot;&nbsp;x" },
    { raw: "[collapse=标题 <x>]内容[/collapse] [collapse]默认[/collapse]" },
    { raw: "[color=red]红[/color][color=skyblue]蓝[/color][color=#ff0000]十六[/color][color=crimson]c[/color][color=darkgreen]g[/color][color=pink]x[/color][color=red]  [/color][color=Gray]g[/color]" },
    { raw: "[quote][pid=123,1,1]Reply[/pid] [b]Post by [uid=1]甲[/uid] (2020-01-01):[/b]\n原文[/quote]\n正文" },
    { raw: "[quote]普通引用\n第二行[/quote]后文[quote]第二个[/quote]" },
    { raw: "[list][*]一[*]二[/list]\n[list=1][*]甲[/list]" },
    { raw: "[img]./mon_202401/a.png[/img][img]https://img.nga.cn/x.jpg[/img][img]  [/img][img]/abs/y.gif[/img]" },
    { raw: "[url=https://e.com/a b]文本[/url][url=https://e.com/]  [/url][url]https://e.com/p[/url]" },
    { raw: "[h]标题[/h][h][/h][h][b]粗[/b]\n第二[/h]" },
    { raw: "[align=center][b][size=150%]居中标题[/size][/b][/align][align=center]第一行\n第二行[/align][align=center]- 列表样[/align][align=center]http://a.b[/align][align=center][/align]" },
    { raw: "[size=150%][b]大标题[/b][/size] [b][size=130%]另一[/size][/b] [size=100%][b]小[/b][/size] [size=200%][b]" + "长".repeat(60) + "[/b][/size] [size=abc][b]x[/b][/size]" },
    { raw: "[b]粗体[/b][b]多行\n第二行[/b][b]" + "长".repeat(210) + "\n二[/b] [i]斜\n体[/i][u]下划[/u][del]删[/del][strikeout]删2[/strikeout][b] [/b][i] [/i]" },
    { raw: "[font=黑体]字[/font][size=100%]s[/size][s:ac:哭]残留[l][/l][unknown=1]x[/unknown][sticker]" },
    { raw: "未闭合[b]粗 [quote]嵌套[b]b[/b][/quote]" },
    { raw: "[img]https://x/a.png[/img][img]./rel/b.png[/img]", attachBase: "https://img2.nga.cn/att/", imageMap: { "https://x/a.png": "local-image://a1.png", "https://img2.nga.cn/att/rel/b.png": "local-image://b1.png" } },
    { raw: "" },
    { raw: "   \n\n  " },
    { raw: "a\n\n\n\n\nb" },
    { raw: "[tid=1]主题[/tid][pid=2]帖[/pid][uid=3]用户[/uid][@甲][flash]x[/flash][code]c[/code]" },
    { raw: "[b][color=red]嵌套色粗[/color][/b][size=140%][color=blue]大蓝[/color][/size]" },
    { raw: "[quote][b]Post by [uid]丁[/uid][/b]\n[img]./a.png[/img]内容[/quote]" },
  ];
  for (const [i, b] of bbcodes.entries()) {
    const opts: any = {};
    if (b.attachBase) opts.attachBase = b.attachBase;
    if (b.imageMap) opts.imageMap = new Map(Object.entries(b.imageMap));
    add("ngaBbcode", `bbcode-${i}`, b, T.ngaBbcodeToMarkdown(b.raw, opts));
  }

  const ctxs = [
    "[quote][pid=123,1,1]Reply[/pid] [b]Post by [uid=1]甲[/uid] (2020-01-01 10:00):[/b]\n被回复内容[/quote]\n我的回复",
    "[quote]普通引用[/quote]回复",
    "无引用",
    "  [quote][pid=77]Reply[/pid][/quote] 只有 pid",
    "[quote]Post by 乙 (2020)\n内容[/quote]x",
    "[quote]Reply to Reply Post by 丙(#12) 内容[/quote]x",
    "[quote][b]Post by [uid]丁[/uid][/b][/quote]",
    "[quote][pid=5][/pid][/quote]后文",
    "[quote]Post by[/quote]",
    "[quote][pid=8,2,3]Reply[/pid] [b]Post by [uid=9]己[/uid] (t):[/b]" + "很长的内容".repeat(60) + "[/quote]回复",
    "&lt;br&gt;[quote]带实体[/quote]\r\n尾",
  ];
  for (const [i, raw] of ctxs.entries()) {
    const c = T.extractNgaReplyContext(raw);
    add("ngaReplyCtx", `replyctx-${i}`, raw, { content: c.content, replyTo: c.replyTo ?? null });
  }

  const stores = [
    "window.script_muti_get_var_store={\"data\":{\"__T\":{\"tid\":1}},\"encode\":\"gbk\"};",
    "window.script_muti_get_var_store={\"data\":{\"x\":\"a\tb\nc\"}};;  \n",
    "{\"data\":{}}",
    "{\"error\":{\"0\":\"15 guest\"}}",
    "not json at all",
    "window.script_muti_get_var_store=",
    "{\"data\":{\"q\":\"带\\\"转义\\\"引号\tx\"}}",
    "{\"data\":{\"k\":\"v\u0001w\u001fz\b\f\r\"}}",
    "",
  ];
  for (const [i, s] of stores.entries()) {
    let out: unknown;
    try { out = { ok: T.parseNgaStore(s) }; } catch (e) { out = { error: e instanceof Error ? e.message : String(e) }; }
    add("ngaStore", `store-${i}`, s, out);
  }
  for (const [i, s] of ["\"a\tb\n\"", "{\"k\":\"v\u0001w\"}", "\"\\\"\tq\\\\\"\t\"x\ty\"", "\"\b\f\u0000\"", "no strings", "\"unterminated\tstring"].entries()) {
    add("ngaEscape", `escape-${i}`, s, T.escapeControlCharsInJsonStrings(s));
  }
  const cookieCases: [string | undefined, string[], string][] = [
    [undefined, [], ""],
    ["a=1; b=2", ["b=3", "c=4"], ""],
    ["a=1", [], "<script>guestJs=abc123;</script>"],
    [" x=1 ; =bad; y", ["z", "w=2=3"], "guestJs='q9'"],
    ["guestJs=old", [], "guestJs=\"new1\""],
    ["", ["only=1"], "no guest here"],
  ];
  for (const [i, [e, sc, body]] of cookieCases.entries()) {
    add("ngaCookies", `cookies-${i}`, { existing: e ?? null, setCookies: sc, body }, T.mergeNgaCookies(e, sc, body));
  }
  for (const [i, [raw, base]] of ([
    ["./mon_1/a.png", "https://img.nga.cn/attachments"],
    ["mon_1/b.png", "https://img.nga.cn/attachments/"],
    ["/mon_1/c.png", "https://img.nga.cn/attachments"],
    ["//x.com/y.png", "https://img.nga.cn/attachments"],
    ["  https://other.com/d.png  ", "https://img.nga.cn/attachments"],
    ["HTTP://UP.com/e.png", "https://img.nga.cn/attachments"],
    ["", "https://img.nga.cn/attachments"],
    ["./", "https://img.nga.cn/attachments"],
    ["   ", "x"],
  ] as [string, string][]).entries()) {
    add("ngaImgUrl", `imgurl-${i}`, { raw, base }, T.resolveNgaImageUrl(raw, base));
  }
  for (const [i, s] of ["a guestJs=abc; b", "guestJs='q'", "guestJs=", "nothing", "x guestJs=一二三\" y"].entries()) {
    add("ngaGuest", `guest-${i}`, s, T.extractGuestJs(s));
  }
  for (const [i, s] of ["普通", "A &amp;amp; B", "&amp;amp;amp;amp;", "&lt;b&gt;粗&lt;/b&gt;", "&#39;单&quot;双&quot;&nbsp;空", "&#x4e2d;&#20013;", "", "&amp;lt;"].entries()) {
    add("ngaSubject", `subject-${i}`, s, T.decodeNgaSubject(s));
  }

  // ---------- 整帖抓取 ----------
  const longReply = "长回复".repeat(30);
  const cases: ThreadCase[] = [];
  const opPost = (lou = 0) => post(lou, 11, "主楼正文 [img]./mon_1/a.png[/img] 和 [b]粗体[/b]", {
    subject: "主楼标题",
    attachs: { "0": { attachurl: "mon_1/a.png", type: "img" }, "1": { attachurl: "mon_1/orphan.jpg", type: "img" }, "2": { attachurl: "mon_1/f.zip", type: "zip" }, "3": { type: "img" } },
  });
  cases.push({
    name: "nga-basic", tid: "100", retryDelays: [], images: { "https://img.nga.cn/attachments/mon_1/a.png": PNG },
    fixture: {
      [pg("100", 1)]: store(pageData({ __T: topic(), __ROWS: 44, __R: [opPost(), post(1, 22, "路人一楼"), post(2, 11, "[quote][pid=555,1,1]Reply[/pid] [b]Post by [uid=22]路人甲[/uid] (2023-11-14 22:13):[/b]\n\n原话[/quote]\n楼主回复")] })),
      [pg("100", 2)]: store(pageData({ __R: { "0": post(20, 33, "第二页"), "1": post(21, 11, "楼主二十一楼 [img]./mon_1/b.png[/img]") } })),
      [pg("100", 3)]: store(pageData({ __R: [post(40, 22, "第三页")] })),
      [pg("100", 1, 11)]: store(pageData({ __R: [opPost(), post(2, 11, "[quote][pid=555,1,1]Reply[/pid] [b]Post by [uid=22]路人甲[/uid] (2023-11-14 22:13):[/b]\n\n原话[/quote]\n楼主回复"), post(21, 11, "楼主二十一楼 [img]./mon_1/b.png[/img]")] })),
    },
  });
  cases.push({
    name: "nga-guest-handshake", tid: "101", retryDelays: [],
    fixture: {
      [pg("101", 1)]: [
        { status: 403, text: "<script>document.cookie='guestJs=xyz123;path=/'</script>", setCookies: ["lastvisit=1"] },
        store(pageData({ __T: topic({ tid: 101, replies: 0 }), __ROWS: 1, __R: [post(0, 11, "握手后的主楼")] }), ),
      ],
      [pg("101", 1, 11)]: store(pageData({ __R: [post(0, 11, "握手后的主楼")] })),
    },
  });
  cases.push({ name: "nga-guest-denied", tid: "102", retryDelays: [], fixture: { [pg("102", 1)]: { status: 403, text: "forbidden" } } });
  cases.push({ name: "nga-err-15", tid: "103", retryDelays: [], fixture: { [pg("103", 1)]: store(undefined, { "0": "15 访客" }) } });
  cases.push({ name: "nga-err-5", tid: "104", retryDelays: [], fixture: { [pg("104", 1)]: store(undefined, { "0": "5xx" }) } });
  cases.push({ name: "nga-err-other", tid: "105", retryDelays: [], fixture: { [pg("105", 1)]: store(undefined, { "0": "7", "1": "<b>违规</b>内容 " + "长".repeat(100) }) } });
  cases.push({ name: "nga-err-other-nodetail", tid: "106", retryDelays: [], fixture: { [pg("106", 1)]: store(undefined, { "0": "9" }) } });
  cases.push({ name: "nga-msg-15", tid: "107", retryDelays: [], fixture: { [pg("107", 1)]: store({ __MESSAGE: { "0": 15, "1": "x" } }) } });
  cases.push({ name: "nga-msg-other", tid: "108", retryDelays: [], fixture: { [pg("108", 1)]: store({ __MESSAGE: { "0": 3, "1": "被屏蔽 " + "长".repeat(100) } }) } });
  cases.push({ name: "nga-msg-nodetail", tid: "109", retryDelays: [], fixture: { [pg("109", 1)]: store({ __MESSAGE: { "0": 4 } }) } });
  cases.push({ name: "nga-nodata", tid: "110", retryDelays: [], fixture: { [pg("110", 1)]: { text: "window.script_muti_get_var_store={\"encode\":\"gbk\"};" } } });
  cases.push({ name: "nga-unparsable", tid: "111", retryDelays: [], fixture: { [pg("111", 1)]: { text: "<html>garbage</html>" } } });
  cases.push({ name: "nga-no-topic", tid: "112", retryDelays: [], fixture: { [pg("112", 1)]: store(pageData({ __R: [post(0, 11, "x")] })) } });
  cases.push({ name: "nga-no-posts", tid: "113", retryDelays: [], fixture: { [pg("113", 1)]: store(pageData({ __T: topic({ tid: 113 }), __R: [] })), [pg("113", 1, 11)]: store(pageData({ __R: [] })) } });
  for (const [i, e] of ["HTTP 404", "HTTP 503", "请求超时", "连接被中断", "奇怪的错误", "已取消"].entries()) {
    cases.push({ name: `nga-fetch-err-${e}`, tid: `12${i}`, retryDelays: [], fixture: { [pg(`12${i}`, 1)]: { error: e } } });
  }
  cases.push({
    name: "nga-retry-ok", tid: "130", retryDelays: [0, 0],
    fixture: {
      [pg("130", 1)]: [{ error: "HTTP 502" }, { error: "请求超时" }, store(pageData({ __T: topic({ tid: 130 }), __ROWS: 1, __R: [post(0, 11, "重试后主楼")] }))],
      [pg("130", 1, 11)]: store(pageData({ __R: [post(0, 11, "重试后主楼")] })),
    },
  });
  cases.push({
    name: "nga-retry-exhausted", tid: "131", retryDelays: [0],
    fixture: { [pg("131", 1)]: [{ error: "HTTP 500" }, { error: "HTTP 500" }] },
  });
  cases.push({
    name: "nga-sample-page-fail", tid: "132", retryDelays: [],
    fixture: {
      [pg("132", 1)]: store(pageData({ __T: topic({ tid: 132 }), __ROWS: 100, __R: [post(0, 11, "主楼"), post(1, 22, "一楼")] })),
      [pg("132", 2)]: store(pageData({ __R: [post(20, 22, "二页")] })),
      [pg("132", 3)]: { error: "HTTP 500" },
      [pg("132", 1, 11)]: store(pageData({ __R: [post(0, 11, "主楼")] })),
    },
  });
  cases.push({
    name: "nga-summary-cap", tid: "133", retryDelays: [], maxSummaryPages: 2,
    fixture: {
      [pg("133", 1)]: store(pageData({ __T: topic({ tid: 133 }), __ROWS: 100, __R: [post(0, 11, "主楼"), post(1, 22, "一楼")] })),
      [pg("133", 2)]: store(pageData({ __R: [post(20, 22, "二页")] })),
      [pg("133", 1, 11)]: store(pageData({ __R: [post(0, 11, "主楼")] })),
    },
  });
  cases.push({
    name: "nga-op-fetch-fail", tid: "134", retryDelays: [],
    fixture: {
      [pg("134", 1)]: store(pageData({ __T: topic({ tid: 134, replies: 3 }), __ROWS: 3, __R: [post(0, 11, "主楼"), post(1, 22, "路人"), post(2, 11, "楼主回复二")] })),
      [pg("134", 1, 11)]: { error: "HTTP 500" },
    },
  });
  cases.push({
    name: "nga-op-paging", tid: "135", retryDelays: [], maxOpPages: 2,
    fixture: {
      [pg("135", 1)]: store(pageData({ __T: topic({ tid: 135, replies: 60 }), __ROWS: 40, __R: [post(0, 11, "主楼"), post(1, 22, "一")] })),
      [pg("135", 2)]: store(pageData({ __R: [post(20, 22, "二")] })),
      [pg("135", 1, 11)]: store(pageData({ __R: Array.from({ length: 20 }, (_, i) => post(i, 11, `楼主发言${i}`)) })),
      [pg("135", 2, 11)]: store(pageData({ __R: Array.from({ length: 20 }, (_, i) => post(20 + i, 11, `楼主发言${20 + i}`)) })),
      [pg("135", 3, 11)]: store(pageData({ __R: [post(40, 11, "不该被请求")] })),
    },
  });
  cases.push({
    name: "nga-no-authorid", tid: "136", retryDelays: [],
    fixture: { [pg("136", 1)]: store(pageData({ __T: topic({ tid: 136, authorid: undefined }), __ROWS: 2, __R: [post(0, undefined, "匿名主楼", { author: " 直写作者 " }), post(1, -1, "负 uid 楼")] })) },
  });
  cases.push({
    name: "nga-authorid-from-sample", tid: "137", retryDelays: [],
    fixture: {
      [pg("137", 1)]: store(pageData({ __T: topic({ tid: 137, authorid: undefined, subject: undefined }), __ROWS: 2, __R: [post(0, 22, "采样主楼", { subject: "主楼自带标题" }), post(1, 33, "x")] })),
      [pg("137", 1, 22)]: store(pageData({ __R: [post(0, 22, "采样主楼", { subject: "主楼自带标题" })] })),
    },
  });
  cases.push({
    name: "nga-op-missing-lou0", tid: "138", retryDelays: [],
    fixture: {
      [pg("138", 1)]: store(pageData({ __T: topic({ tid: 138, subject: "", replies: 2 }), __ROWS: 2, __R: [post(0, 11, "采样里的主楼 [img]./mon_1/a.png[/img]"), post(1, 22, "路人")] })),
      [pg("138", 1, 11)]: store(pageData({ __R: [post(3, 11, "仅有楼主三楼")] })),
    },
    images: { "https://img.nga.cn/attachments/mon_1/a.png": PNG },
  });
  cases.push({
    name: "nga-parent-fetch", tid: "139", retryDelays: [],
    fixture: {
      [pg("139", 1)]: store(pageData({ __T: topic({ tid: 139, replies: 4 }), __ROWS: 4, __R: [post(0, 11, "主楼"), post(1, 22, "父楼内容 [b]加粗[/b]", { pid: 701 })] })),
      [pg("139", 1, 11)]: store(pageData({ __R: [
        post(0, 11, "主楼"),
        post(2, 11, "[quote][pid=701,1,1]Reply[/pid] [b]Post by [uid=22]路人甲[/uid] (t):[/b][/quote]\n回复一"),
        post(3, 11, "[quote][pid=701,1,1]Reply[/pid][/quote]回复二（同一父楼，命中缓存）"),
        post(4, 11, "[quote][pid=702,9,1]Reply[/pid] [b]Post by [uid=44]某人[/uid] (t):[/b][/quote]\n回复三（补拉失败）"),
        post(5, 11, "[quote][pid=703]Reply[/pid][/quote]回复四（仅 pid，失败后无作者）"),
      ] })),
      [pidU(701)]: store(pageData({ __R: [post(1, 22, "父楼内容 [b]加粗[/b]", { pid: 701 })] })),
      [pidU(703)]: store(pageData({ __R: [] })),
    },
  });
  cases.push({
    name: "nga-parent-fetch-capped", tid: "140", retryDelays: [], maxParentFetches: 1,
    fixture: {
      [pg("140", 1)]: store(pageData({ __T: topic({ tid: 140 }), __ROWS: 3, __R: [post(0, 11, "主楼")] })),
      [pg("140", 1, 11)]: store(pageData({ __R: [post(0, 11, "主楼"), post(2, 11, "[quote][pid=801]Reply[/pid][/quote]甲"), post(3, 11, "[quote][pid=802]Reply[/pid][/quote]乙")] })),
      [pidU(801)]: store(pageData({ __R: [post(1, 22, "第一个父楼", { pid: 801 })] })),
      [pidU(802)]: store(pageData({ __R: [post(1, 22, "第二个父楼", { pid: 802 })] })),
    },
  });
  cases.push({
    name: "nga-parent-guest-403", tid: "141", retryDelays: [],
    fixture: {
      [pg("141", 1)]: store(pageData({ __T: topic({ tid: 141 }), __ROWS: 2, __R: [post(0, 11, "主楼")] })),
      [pg("141", 1, 11)]: store(pageData({ __R: [post(0, 11, "主楼"), post(2, 11, "[quote][pid=900]Reply[/pid][/quote]回复")] })),
      [pidU(900)]: [{ status: 403, text: "guestJs=handshake1" }, store(pageData({ __R: [post(1, 22, "握手后的父楼", { pid: 900 })] }))],
    },
  });
  cases.push({
    name: "nga-images-limit", tid: "142", retryDelays: [], imageLimit: 2,
    images: { "https://img.nga.cn/attachments/a1.png": PNG, "https://img.nga.cn/attachments/a2.png": PNG, "https://img.nga.cn/attachments/a3.png": PNG },
    fixture: {
      [pg("142", 1)]: store(pageData({ __T: topic({ tid: 142 }), __ROWS: 1, __R: [post(0, 11, "[img]./a1.png[/img][img]./a2.png[/img][img]./a3.png[/img][img]./a4.png[/img][img]./a1.png[/img]")] })),
      [pg("142", 1, 11)]: store(pageData({ __R: [post(0, 11, "[img]./a1.png[/img][img]./a2.png[/img][img]./a3.png[/img][img]./a4.png[/img][img]./a1.png[/img]")] })),
    },
  });
  cases.push({
    name: "nga-images-failed", tid: "143", retryDelays: [],
    images: { "https://img.nga.cn/attachments/ok.png": PNG },
    fixture: {
      [pg("143", 1)]: store(pageData({ __T: topic({ tid: 143 }), __ROWS: 1, __R: [post(0, 11, "[img]./ok.png[/img][img]./bad1.png[/img][img]./bad2.png[/img]", { attachs: [{ attachurl: "extra/ok2.png" }, { attachurl: "ok.png", type: "img" }] })] })),
      [pg("143", 1, 11)]: store(pageData({ __R: [post(0, 11, "[img]./ok.png[/img][img]./bad1.png[/img][img]./bad2.png[/img]", { attachs: [{ attachurl: "extra/ok2.png" }, { attachurl: "ok.png", type: "img" }] })] })),
    },
  });
  cases.push({
    name: "nga-attach-base-http", tid: "144", retryDelays: [],
    fixture: {
      [pg("144", 1)]: store(pageData({ __T: topic({ tid: 144 }), __ROWS: 1, __GLOBAL: { _ATTACH_BASE_VIEW: "https://cdn.example.com/att/" }, __R: [post(0, 11, "[img]./p.png[/img]")] })),
      [pg("144", 1, 11)]: store(pageData({ __R: [post(0, 11, "[img]./p.png[/img]")] })),
    },
  });
  cases.push({
    name: "nga-fid-root-name", tid: "145", retryDelays: [],
    fixture: {
      [pg("145", 1)]: store(pageData({ __T: topic({ tid: 145, fid: 99 }), __F: { name: " 根名 " }, __ROWS: 1, __R: [post(0, 11, "主楼")] })),
      [pg("145", 1, 11)]: store(pageData({ __R: [post(0, 11, "主楼")] })),
    },
  });
  cases.push({
    name: "nga-empty-reply-skipped", tid: "146", retryDelays: [],
    fixture: {
      [pg("146", 1)]: store(pageData({ __T: topic({ tid: 146, postdate: undefined }), __ROWS: 3, __R: [post(0, 11, "主楼")] })),
      [pg("146", 1, 11)]: store(pageData({ __R: [post(0, 11, "主楼"), post(2, 11, ""), post(3, 11, "[color=red][/color]"), post(4, 11, "[quote]只有引用[/quote]"), post(5, 11, longReply)] })),
    },
  });

  for (const c of cases) {
    const calls: unknown[] = [];
    const imageCalls: string[] = [];
    const counters: Record<string, number> = {};
    const deps: Record<string, unknown> = {
      retryDelaysMs: c.retryDelays ?? [],
      fetchRawText: async (url: string, options: any = {}) => {
        calls.push({ url, cookie: options.cookie ?? null, accept403: Array.isArray(options.acceptStatuses) && options.acceptStatuses.includes(403), ua: options.userAgent ?? null, referer: options.referer ?? null });
        const entry = c.fixture[url];
        if (!entry) throw new Error("HTTP 404");
        const list = Array.isArray(entry) ? entry : [entry];
        const i = Math.min(counters[url] ?? 0, list.length - 1);
        counters[url] = (counters[url] ?? 0) + 1;
        const o = list[i];
        if (o.error !== undefined) throw new Error(o.error);
        return { status: o.status ?? 200, text: o.text ?? "", contentType: "text/plain", finalUrl: url, setCookies: o.setCookies ?? [] };
      },
      downloadImage: async (url: string) => {
        imageCalls.push(url);
        const b64 = c.images?.[url];
        if (b64 === undefined) throw new Error("HTTP 404");
        const dir = fs.mkdtempSync(path.join(os.tmpdir(), "nga-golden-"));
        const filePath = path.join(dir, "image.png");
        fs.writeFileSync(filePath, Buffer.from(b64, "base64"));
        return { dir, filePath };
      },
      saveImageAsset: async (filePath: string) => "img-" + crypto.createHash("sha1").update(fs.readFileSync(filePath)).digest("hex").slice(0, 8) + ".png",
    };
    if (c.imageLimit !== undefined) deps.imageLimit = c.imageLimit;
    if (c.maxSummaryPages !== undefined) deps.maxSummaryPages = c.maxSummaryPages;
    if (c.maxOpPages !== undefined) deps.maxOpPages = c.maxOpPages;
    if (c.maxParentFetches !== undefined) deps.maxParentFetches = c.maxParentFetches;
    let thread: unknown, error: string | undefined;
    try {
      thread = await T.fetchNgaThread(c.tid, deps);
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    }
    add("ngaThread", c.name, { tid: c.tid, fixture: c.fixture, images: c.images ?? {}, retryDelays: c.retryDelays ?? [], imageLimit: c.imageLimit ?? null, maxSummaryPages: c.maxSummaryPages ?? null, maxOpPages: c.maxOpPages ?? null, maxParentFetches: c.maxParentFetches ?? null },
      { thread: norm(thread ?? null), error: error ?? null, calls, imageCalls });
  }
}
