// 小红书连接器差分夹具：真实 xiaohongshu.ts（tsx 直接加载）跑在脚本化的笔记页上。
// 由 golden-video.mts 动态引入；kind 均以 "xhs" 开头，Go 端由 video_xiaohongshu_test.go 对拍。

/* eslint-disable @typescript-eslint/no-explicit-any */
type Add = (kind: string, name: string, input: unknown, output: unknown) => void;
const errMsg = (e: unknown) => (e instanceof Error ? e.message : String(e));

const NID = "65f1a2b3c4d5e6f7a8b9c0d1";
const U = "@@undef@@";
/** 序列化成 __INITIAL_STATE__ 的形态：U 占位符变成字符串外的裸 undefined */
export const stateHtml = (state: unknown, tail = "") =>
  `<html><script>var z = {a: "}"};</script><script>window.__INITIAL_STATE__=${JSON.stringify(state).replace(new RegExp(`"${U}"`, "g"), "undefined")}${tail}</script></html>`;
const stateOf = (note: Record<string, unknown> | null, id = NID, extra: Record<string, unknown> = {}) => ({
  other: U, note: { firstNoteId: U, noteDetailMap: { [id]: note === null ? null : { note: { noteId: id, ...note } } }, ...extra },
});
const stream = (over: Record<string, unknown> = {}) => ({ masterUrl: "http://v.example/1.mp4", backupUrls: ["http://v.example/1b.mp4", "https://v.example/1.mp4"], duration: 12500, ...over });
const videoNote = (over: Record<string, unknown> = {}) => ({
  type: "video", title: "视频标题", desc: "视频文案", user: { nickname: " 作者 " },
  video: { media: { stream: { h264: [stream()], h265: [stream({ masterUrl: "http://v.example/2.mp4", backupUrls: [], duration: undefined })], av1: "x" } }, capa: { duration: 30 } }, ...over,
});
const imageNote = (over: Record<string, unknown> = {}) => ({
  type: "normal", title: "图文标题", desc: "图文文案 #AI漫剧[话题]# 与 #x y[话题]#", user: { nickName: "备用昵称" },
  imageList: [
    { urlDefault: "http://i.example/1!nd_dft", infoList: [{ url: "http://i.example/1a" }, null, { url: "https://i.example/1a" }, {}], urlPre: "http://i.example/pre1" },
    { urlDefault: "" }, { infoList: [] }, { urlDefault: "https://i.example/3", urlPre: "https://i.example/3" },
  ], ...over,
});

export async function addXhsCases(add: Add): Promise<void> {
  const X: any = await import("../../apps/desktop/src/main/services/import/xiaohongshu.ts");

  const urls = [
    `https://www.xiaohongshu.com/explore/${NID}`, `https://www.xiaohongshu.com/explore/${NID.toUpperCase()}?xsec_token=abc`,
    `https://www.xiaohongshu.com/discovery/item/${NID}`, `https://www.xiaohongshu.com/explore/${NID.slice(0, 15)}`, "https://xhslink.com/a/abc",
    "not a url", "", `  https://www.xiaohongshu.com/explore/${NID}  `, `https://www.xiaohongshu.com/user/profile/x/${NID}`,
    `https://www.xiaohongshu.com/explore/${NID}0123456789`,
  ];
  for (const [i, u] of urls.entries()) add("xhsId", `id-${i}`, u, X.extractXhsNoteId(u));
  add("xhsUrl", "note-url", NID, X.xiaohongshuNoteUrl(NID));

  const texts = ["", "#AI漫剧[话题]#", "前 #a[话题]# 中 #b c[话题]# 后", "#[话题]#", "##x[话题]#", "#跨\n行[话题]#", "#a[话题]##b[话题]#", "#a[话题]", "无话题 undefined"];
  for (const [i, t] of texts.entries()) add("xhsClean", `clean-${i}`, t, X.cleanNoteText(t));

  const slices = [
    `window.__INITIAL_STATE__={"a":undefined,"b":"undefined","c":[undefined,{"d":undefined}]}`,
    `window.__INITIAL_STATE__ = {"s":"含 } 与 { 及 \\" 转义","u":undefined};var x=1`,
    `x window.__INITIAL_STATE__=\n{"nested":{"deep":{"v":undefined}}}`, `window.__INITIAL_STATE__ = null`, `没有标记 {"a":1}`, `window.__INITIAL_STATE__={"a":1`,
    `window.__INITIAL_STATE__={"uundefinedu":1,"k":undefinedundefined}`, `window.__INITIAL_STATE__={"a":"undefined\\\\"}`, `window.__INITIAL_STATE__={undefined}`,
  ];
  for (const [i, s] of slices.entries()) add("xhsSlice", `slice-${i}`, s, X.sliceInitialStateJson(s));

  const mkVideo = (over: Record<string, unknown> = {}) => videoNote(over);
  const cases: { name: string; html: string; id?: string | null }[] = [
    { name: "video", html: stateHtml(stateOf(mkVideo())), id: NID },
    { name: "video-capa-rounding", html: stateHtml(stateOf(mkVideo({ video: { media: { stream: { a: [stream({ duration: undefined })] } }, capa: { duration: 30.5 } } }))), id: NID },
    { name: "video-stream-duration-half", html: stateHtml(stateOf(mkVideo({ video: { media: { stream: { a: [stream({ duration: 500 })] } } } }))), id: NID },
    { name: "video-no-duration", html: stateHtml(stateOf(mkVideo({ video: { media: { stream: { a: [stream({ duration: "5" })] } } } }))), id: NID },
    { name: "video-no-media", html: stateHtml(stateOf(mkVideo({ video: null }))), id: NID },
    { name: "video-no-streams-array", html: stateHtml(stateOf(mkVideo({ video: { media: { stream: { a: "x", b: 5 } }, capa: { duration: 12 } } }))), id: NID },
    { name: "video-backup-not-array", html: stateHtml(stateOf(mkVideo({ video: { media: { stream: { a: [{ masterUrl: " http://m/1 ", backupUrls: "x" }] } } } }))), id: NID },
    { name: "video-null-stream", html: stateHtml(stateOf(mkVideo({ video: { media: { stream: { a: [null] } } } }))), id: NID },
    { name: "video-with-imagelist-ignored", html: stateHtml(stateOf(mkVideo({ imageList: [{ urlDefault: "http://cover" }] }))), id: NID },
    { name: "note", html: stateHtml(stateOf(imageNote())), id: NID },
    { name: "note-no-type", html: stateHtml(stateOf(imageNote({ type: undefined }))), id: NID },
    { name: "note-image-null", html: stateHtml(stateOf(imageNote({ imageList: [null] }))), id: NID },
    { name: "note-imagelist-null", html: stateHtml(stateOf(imageNote({ imageList: null }))), id: NID },
    { name: "note-infolist-null", html: stateHtml(stateOf(imageNote({ imageList: [{ urlDefault: "http://a", infoList: null }] }))), id: NID },
    { name: "title-empty-fallback", html: stateHtml(stateOf(imageNote({ title: "  ", desc: "\r\n 文案首行 \r\n后续" }))), id: NID },
    { name: "title-empty-long", html: stateHtml(stateOf(imageNote({ title: "", desc: "长".repeat(130) }))), id: NID },
    { name: "title-empty-no-desc", html: stateHtml(stateOf(imageNote({ title: "", desc: "" }))), id: NID },
    { name: "desc-equals-title", html: stateHtml(stateOf(imageNote({ title: "同", desc: "同" }))), id: NID },
    { name: "desc-equals-title-after-clean", html: stateHtml(stateOf(imageNote({ title: "#a[话题]#", desc: "#a[话题]#" }))), id: NID },
    { name: "undefined-in-text", html: stateHtml(stateOf(imageNote({ desc: "变量是 undefined 的情况", title: "undefined 标题" }))), id: NID },
    { name: "user-null", html: stateHtml(stateOf(imageNote({ user: null }))), id: NID },
    { name: "nickname-wins", html: stateHtml(stateOf(imageNote({ user: { nickname: "甲", nickName: "乙" } }))), id: NID },
    { name: "id-from-url-arg", html: stateHtml(stateOf(imageNote(), "aaaaaaaaaaaaaaaaaaaaaaaa")), id: "aaaaaaaaaaaaaaaaaaaaaaaa" },
    { name: "id-arg-not-in-map-uses-first-note", html: stateHtml({ note: { firstNoteId: NID, noteDetailMap: { [NID]: { note: imageNote() }, other: { note: imageNote({ title: "别的" }) } } } }), id: "bbbbbbbbbbbbbbbbbbbbbbbb" },
    { name: "first-key-fallback", html: stateHtml({ note: { noteDetailMap: { zzz: { note: imageNote({ title: "z" }) }, "5": { note: imageNote({ title: "五" }) } } } }), id: null },
    { name: "first-note-without-note-skipped", html: stateHtml({ note: { firstNoteId: "x1", noteDetailMap: { x1: { note: null }, [NID]: { note: imageNote() } } } }), id: null },
    { name: "empty-map", html: stateHtml({ note: { noteDetailMap: {} } }), id: NID },
    { name: "map-entry-null", html: stateHtml(stateOf(null)), id: NID },
    { name: "no-note-state", html: stateHtml({ other: 1 }), id: NID },
    { name: "note-state-null", html: stateHtml({ note: null }), id: NID },
    { name: "map-not-object", html: stateHtml({ note: { noteDetailMap: "x" } }), id: NID },
    { name: "note-truthy-nonobject", html: stateHtml({ note: { noteDetailMap: { [NID]: { note: 5 } } } }), id: NID },
    { name: "invalid-json", html: `<script>window.__INITIAL_STATE__={a:}</script>`, id: NID },
    { name: "marker-missing", html: "<html></html>", id: NID },
    { name: "marker-no-brace", html: "<script>window.__INITIAL_STATE__ = 1</script>", id: NID },
    { name: "trailing-code", html: stateHtml(stateOf(imageNote()), "\nvar late = {};"), id: NID },
  ];
  for (const c of cases) {
    let out: unknown;
    const warn = console.warn, error = console.error, log = console.log;
    console.warn = console.error = console.log = () => {};
    try {
      out = { ok: X.parseXiaohongshuNote(c.html, c.id ?? null) };
    } catch (e) {
      out = { error: errMsg(e), code: (e as any)?.name === "PlatformParseError" ? (e as any).code : null };
    } finally { console.warn = warn; console.error = error; console.log = log; }
    add("xhsParse", c.name, { html: c.html, id: c.id ?? null }, out);
  }

  type PageOutcome = { html: string; finalUrl: string } | { error: string };
  const good = stateHtml(stateOf(imageNote()));
  const shortUrl = "https://xhslink.com/a/abc";
  const full = `https://www.xiaohongshu.com/explore/${NID}?xsec_token=T`;
  const scenarios: { name: string; url: string; pages: Record<string, PageOutcome> }[] = [
    { name: "direct", url: full, pages: { [full]: { html: good, finalUrl: full } } },
    { name: "short-link-redirect", url: shortUrl, pages: { [shortUrl]: { html: good, finalUrl: full } } },
    { name: "id-from-input-url", url: full, pages: { [full]: { html: good, finalUrl: "https://www.xiaohongshu.com/other" } } },
    { name: "404-redirect", url: full, pages: { [full]: { html: good, finalUrl: "https://www.xiaohongshu.com/404?source=x" } } },
    { name: "404-subpath", url: full, pages: { [full]: { html: good, finalUrl: "https://www.xiaohongshu.com/404/sec_xxx" } } },
    { name: "bad-final-url", url: full, pages: { [full]: { html: good, finalUrl: "" } } },
    { name: "fetch-error", url: full, pages: { [full]: { error: "HTTP 461" } } },
    { name: "structure-missing", url: full, pages: { [full]: { html: "<html></html>", finalUrl: full } } },
  ];
  for (const s of scenarios) {
    const fetchCalls: string[] = [];
    const deps = {
      fetchPage: async (u: string) => {
        fetchCalls.push(u);
        const r = s.pages[u];
        if (!r) throw new Error(`未脚本化的地址 ${u}`);
        if ("error" in r) throw new Error(r.error);
        return r;
      },
    };
    let out: unknown;
    const warn = console.warn, error = console.error, log = console.log;
    console.warn = console.error = console.log = () => {};
    try {
      out = { ok: await X.fetchXiaohongshuNote(s.url, undefined, deps), fetchCalls };
    } catch (e) {
      out = { error: errMsg(e), code: (e as any)?.name === "PlatformParseError" ? (e as any).code : null, fetchCalls };
    } finally { console.warn = warn; console.error = error; console.log = log; }
    add("xhsFetch", s.name, s, out);
  }
}
