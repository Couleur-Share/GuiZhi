import { describe, expect, it } from "vitest";
import { matchesSearchRequest, searchResponseRows } from "../../../src/main/services/platform-capture/search-capture";

const douyin = "https://www.douyin.com/aweme/v1/web/general/search/single/";
const xhs = "https://edith.xiaohongshu.com/api/sns/web/v1/search/notes";
describe("搜索响应归属", () => {
  it("只接受搜索接口中与当前关键词一致的 GET 或 POST 请求", () => {
    expect(matchesSearchRequest("douyin", { url: `${douyin}?keyword=GPT-6` }, { keyword: "ＧＰＴ-6" })).toBe(true);
    expect(matchesSearchRequest("xiaohongshu", { url: xhs, postData: JSON.stringify({ keyword: "GPT-6" }) }, { keyword: "GPT-6" })).toBe(true);
    expect(matchesSearchRequest("xiaohongshu", { url: xhs, postData: "keyword=GPT-6" }, { keyword: "GPT-6" })).toBe(true);
  });
  it.each([
    `${douyin}?keyword=其他主题`,
    douyin,
    "https://www.douyin.com/aweme/v1/web/tab/feed/?keyword=GPT-6",
    "https://www.douyin.com/aweme/v1/web/search/sug/?keyword=GPT-6",
    "https://www.douyin.com/aweme/v1/web/discover/search/?keyword=GPT-6",
    "https://www.douyin.com.evil.test/aweme/v1/web/general/search/single/?keyword=GPT-6",
  ])("排除推荐、热词、其他关键词及非平台域名：%s", (url) => {
    expect(matchesSearchRequest("douyin", { url }, { keyword: "GPT-6" })).toBe(false);
  });
  it("只提取结果数组，过滤广告，区分空结果与错误/未知结构", () => {
    const item = { aweme_id: "result", desc: "GPT-6" };
    expect(searchResponseRows("douyin", { data: [{ aweme_info: item }, { is_ads: true, aweme_info: item }, { aweme_info: { ...item, is_ads: true } }], recommend: [item] })).toEqual([item]);
    const note = { id: "note-1", note_card: { display_title: "GPT-6" } };
    expect(searchResponseRows("xiaohongshu", { code: 0, data: { items: [note] } })).toEqual([note]);
    expect(searchResponseRows("douyin", { data: [] })).toEqual([]);
    expect(searchResponseRows("xiaohongshu", { success: false, data: { items: [] } })).toBeNull();
    expect(searchResponseRows("douyin", { status_code: 10001, data: [] })).toBeNull();
    expect(searchResponseRows("douyin", { feed: [item] })).toBeNull();
  });
});
