// 抖音连接器差分夹具：真实 douyin.ts（tsx 直接加载）跑在脚本化的分享页与离屏回退上。
// 由 golden-video.mts 动态引入；kind 均以 "douyin" 开头，Go 端由 video_douyin_test.go 对拍。

/* eslint-disable @typescript-eslint/no-explicit-any */
type Add = (kind: string, name: string, input: unknown, output: unknown) => void;
const errMsg = (e: unknown) => (e instanceof Error ? e.message : String(e));

const ID = "7000000000001";
export const routerHtml = (payload: unknown, tail = "") =>
  `<html><head><script>var a = {x: "}"};</script><script>window._ROUTER_DATA = ${typeof payload === "string" ? payload : JSON.stringify(payload)};${tail}</script></head><body>噪声 { } "</body></html>`;
const page = (items: unknown[], extra: Record<string, unknown> = {}) => ({ loaderData: { [`video_(id)/page`]: { videoInfoRes: { item_list: items, ...extra } } } });
const video = (over: Record<string, unknown> = {}) => ({
  aweme_id: ID, desc: "首行\n第二行", author: { nickname: " 昵称 " },
  video: { duration: 12500, play_addr: { url_list: ["https://a.example/aweme/v1/playwm/?x=1", "https://b.example/aweme/v1/playwm/?x=1"] } }, ...over,
});

export async function addDouyinCases(add: Add): Promise<void> {
  const D: any = await import("../../apps/desktop/src/main/services/import/douyin.ts");

  const urls = [
    "https://www.douyin.com/video/7000000000001", "https://www.douyin.com/note/7000000000002?x=1", "https://www.douyin.com/slides/7000000000003",
    "https://www.douyin.com/discover?modal_id=7000000000004", "https://www.douyin.com/discover?modal_id=123", "https://www.douyin.com/discover?modal_id=abc1234567",
    "https://v.douyin.com/abcdef/", "  https://www.douyin.com/video/7000000000005  ", "not a url", "", "https://www.iesdouyin.com/share/video/7000000000006/",
    "https://www.douyin.com/video/12345", "https://www.douyin.com/user/xx?modal_id=7000000000007", "https://www.douyin.com/video/7000000000008/note/7000000000009",
  ];
  for (const [i, u] of urls.entries()) add("douyinId", `id-${i}`, u, D.extractAwemeId(u));
  for (const id of ["7000000000001", "x"]) add("douyinShare", `share-${id}`, id, D.douyinShareUrl(id));
  for (const [i, u] of ["https://c/a.JPG", "https://c/a.png?x=1", "https://c/a.heic", "https://c/a", "https://c/a.webp/", "bad", "https://c/x.jpeg#f", "https://c/.bmp", "https://c/a.GIF", "https://c/a."].entries()) {
    add("douyinExt", `ext-${i}`, u, D.imageExtensionFromUrl(u));
  }

  const longDesc = "超长".repeat(70) + "尾";
  const emojiDesc = "😀".repeat(70);
  const cases: { name: string; html: string; id?: string }[] = [
    { name: "video", html: routerHtml(page([video()])) },
    { name: "note-images", html: routerHtml(page([video({ images: [{ url_list: ["https://i/1.jpg", "", 5, "https://i/1b.jpg"] }, { url_list: [] }, { url_list: "x" }, null, { url_list: ["https://i/3.webp"] }] })])) },
    { name: "desc-equals-title", html: routerHtml(page([video({ desc: "  单行文案  " })])) },
    { name: "desc-long", html: routerHtml(page([video({ desc: longDesc })])) },
    { name: "desc-long-emoji", html: routerHtml(page([video({ desc: emojiDesc })])) },
    { name: "desc-empty", html: routerHtml(page([video({ desc: "" })])) },
    { name: "desc-leading-blank", html: routerHtml(page([video({ desc: "\r\n  \r\n  真正首行  \r\n后文" })])) },
    { name: "desc-nonstring", html: routerHtml(page([video({ desc: 5, author: { nickname: 7 } })])) },
    { name: "multi-items-match", html: routerHtml(page([video({ aweme_id: "999", desc: "别人" }), video({ desc: "命中" })])) },
    { name: "single-item-no-id", html: routerHtml(page([video({ aweme_id: undefined, desc: "无ID单项" })])) },
    { name: "single-item-other-id", html: routerHtml(page([video({ aweme_id: "888" })])) },
    { name: "single-item-null", html: routerHtml(page([null])) },
    { name: "multi-items-nomatch", html: routerHtml(page([video({ aweme_id: "1" }), video({ aweme_id: "2" })])) },
    { name: "filter-detail", html: routerHtml(page([], { filter_list: [{ detail_msg: " 作品已被删除 ", notice: "不用" }] })) },
    { name: "filter-notice", html: routerHtml(page([], { filter_list: [{ notice: "仅自己可见" }] })) },
    { name: "filter-empty-reason", html: routerHtml(page([], { filter_list: [{}] })) },
    { name: "filter-with-items", html: routerHtml(page([video({ aweme_id: "5" }), video({ aweme_id: "6" })], { filter_list: [{ detail_msg: "不应采用" }] })) },
    { name: "no-loader-data", html: routerHtml({}) },
    { name: "loader-array", html: routerHtml({ loaderData: [{ videoInfoRes: { item_list: [video()] } }] }) },
    { name: "loader-string", html: routerHtml({ loaderData: "abc" }) },
    { name: "second-page-wins", html: routerHtml({ loaderData: { a: { other: 1 }, b: null, c: { videoInfoRes: { item_list: [video({ desc: "来自c" })] } } } }) },
    { name: "numeric-key-order", html: routerHtml({ loaderData: { z: { videoInfoRes: { item_list: [video({ desc: "z" })] } }, "2": { videoInfoRes: { item_list: [video({ desc: "二" })] } }, "1": { videoInfoRes: { item_list: [] } } } }) },
    { name: "info-string", html: routerHtml({ loaderData: { a: { videoInfoRes: "x" } } }) },
    { name: "info-no-itemlist", html: routerHtml({ loaderData: { a: { videoInfoRes: { item_list: "x" } } } }) },
    { name: "dur-0.5s", html: routerHtml(page([video({ video: { duration: 500, play_addr: { url_list: ["https://a/playwm/"] } } })])) },
    { name: "dur-1499", html: routerHtml(page([video({ video: { duration: 1499 } })])) },
    { name: "dur-1500", html: routerHtml(page([video({ video: { duration: 1500 } })])) },
    { name: "dur-negative", html: routerHtml(page([video({ video: { duration: -1500 } })])) },
    { name: "dur-string", html: routerHtml(page([video({ video: { duration: "12" } })])) },
    { name: "dur-zero", html: routerHtml(page([video({ video: { duration: 0 } })])) },
    { name: "no-video", html: routerHtml(page([video({ video: undefined })])) },
    { name: "video-null", html: routerHtml(page([video({ video: null, author: null })])) },
    { name: "empty-playlist", html: routerHtml(page([video({ video: { duration: 1000, play_addr: { url_list: [] } } })])) },
    { name: "play-two-playwm", html: routerHtml(page([video({ video: { play_addr: { url_list: ["https://a/playwm/playwm/"] } } })])) },
    { name: "braces-in-strings", html: routerHtml(page([video({ desc: "花括号 } { 与 \\ 引号 \" 之后" })]), "\nvar late = {};") },
    { name: "marker-no-brace", html: "<script>window._ROUTER_DATA = null</script>" },
    { name: "marker-missing", html: "<html>无</html>" },
    { name: "invalid-json", html: routerHtml("{a:1}") },
    { name: "unbalanced", html: "<script>window._ROUTER_DATA = {\"a\": {\"b\": 1}" },
    { name: "id-from-page", html: routerHtml(page([video({ aweme_id: " 7000000000001 " })])) },
    { name: "other-awemeid-arg", html: routerHtml(page([video()])), id: "7000000000099" },
  ];
  for (const c of cases) {
    let out: unknown;
    const warn = console.warn, error = console.error, log = console.log;
    console.warn = console.error = console.log = () => {};
    try {
      out = { ok: D.parseDouyinRouterData(c.html, c.id ?? ID) };
    } catch (e) {
      out = { error: errMsg(e), code: (e as any)?.name === "PlatformParseError" ? (e as any).code : null };
    } finally { console.warn = warn; console.error = error; console.log = log; }
    add("douyinParse", c.name, { html: c.html, id: c.id ?? ID }, out);
  }

  // ---------- fetchDouyinAweme：脚本化分享页与离屏回退 ----------
  type PageOutcome = { html: string; finalUrl: string } | { error: string };
  const share = (id: string) => D.douyinShareUrl(id);
  const okPage = (html: string, finalUrl = "") => ({ html, finalUrl });
  const good = routerHtml(page([video()]));
  const broken = "<html>没有数据</html>";
  const scenarios: { name: string; url: string; pages: Record<string, PageOutcome>; capture?: { html: string } | { error: string } }[] = [
    { name: "direct-id", url: `https://www.douyin.com/video/${ID}`, pages: { [share(ID)]: okPage(good) }, capture: { error: "不应调用" } },
    { name: "short-link", url: "https://v.douyin.com/abc/", pages: { "https://v.douyin.com/abc/": okPage("", `https://www.douyin.com/video/${ID}`), [share(ID)]: okPage(good) }, capture: { error: "不应调用" } },
    { name: "short-link-no-id", url: "https://v.douyin.com/abc/", pages: { "https://v.douyin.com/abc/": okPage("", "https://www.douyin.com/") }, capture: { error: "不应调用" } },
    { name: "short-link-fetch-error", url: "https://v.douyin.com/abc/", pages: { "https://v.douyin.com/abc/": { error: "连接超时" } } },
    { name: "share-fetch-error", url: `https://www.douyin.com/video/${ID}`, pages: { [share(ID)]: { error: "HTTP 503" } }, capture: { error: "不应调用" } },
    { name: "fallback-ok", url: `https://www.douyin.com/video/${ID}`, pages: { [share(ID)]: okPage(broken) }, capture: { html: good } },
    { name: "fallback-still-broken", url: `https://www.douyin.com/video/${ID}`, pages: { [share(ID)]: okPage(broken) }, capture: { html: broken } },
    { name: "fallback-error", url: `https://www.douyin.com/video/${ID}`, pages: { [share(ID)]: okPage(broken) }, capture: { error: "离屏页面超时" } },
    { name: "unavailable-no-fallback", url: `https://www.douyin.com/video/${ID}`, pages: { [share(ID)]: okPage(routerHtml(page([], { filter_list: [{ detail_msg: "作品不存在" }] }))) }, capture: { error: "不应调用" } },
  ];
  for (const s of scenarios) {
    const fetchCalls: string[] = [], captureCalls: string[] = [];
    const deps = {
      fetchPage: async (u: string) => {
        fetchCalls.push(u);
        const r = s.pages[u];
        if (!r) throw new Error(`未脚本化的地址 ${u}`);
        if ("error" in r) throw new Error(r.error);
        return r;
      },
      captureDetail: async (id: string) => {
        captureCalls.push(id);
        const r = s.capture!;
        if ("error" in r) throw new Error(r.error);
        return r.html;
      },
    };
    let out: unknown;
    const warn = console.warn, error = console.error, log = console.log;
    console.warn = console.error = console.log = () => {};
    try {
      out = { ok: await D.fetchDouyinAweme(s.url, undefined, deps), fetchCalls, captureCalls };
    } catch (e) {
      out = { error: errMsg(e), code: (e as any)?.name === "PlatformParseError" ? (e as any).code : null, fetchCalls, captureCalls };
    } finally { console.warn = warn; console.error = error; console.log = log; }
    add("douyinFetch", s.name, s, out);
  }
}
