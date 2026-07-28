import { describe, expect, it, vi } from "vitest";

// douyin → safe-fetch → network-proxy 引用 electron，单测中替换为空实现
vi.mock("electron", () => ({
  session: { defaultSession: {} },
  app: {},
}));

import {
  douyinImageNoteSource,
  douyinShareUrl,
  extractAwemeId,
  fetchDouyinAweme,
  imageExtensionFromUrl,
  parseDouyinRouterData,
} from "../../../src/main/services/import/douyin";
import {
  buildImageNoteMetaLine,
  plainTextToMarkdown,
} from "../../../src/main/services/import/image-note-entry";

const AWEME_ID = "7663897644049173802";

interface VideoInfoResFixture {
  item_list?: unknown[];
  filter_list?: unknown[];
}

/** 真实分享页的形态：赋值语句后面还有别的脚本代码 */
function routerDataHtml(videoInfoRes: VideoInfoResFixture): string {
  const payload = {
    loaderData: {
      video_layout: { hasEdge: false },
      "video_(id)/page": { itemId: AWEME_ID, videoInfoRes },
    },
    errors: {},
  };
  return [
    "<!doctype html><html><head>",
    `<script>window._ROUTER_DATA = ${JSON.stringify(payload)};`,
    'window.__INITIAL_SSR__ = {"a":"}}}"};console.log("done");</script>',
    "</head><body></body></html>",
  ].join("");
}

const VIDEO_ITEM = {
  aweme_id: AWEME_ID,
  desc: "2026企业AI落地，终极解决方案是什么？",
  author: { nickname: "曲率出逃" },
  video: {
    duration: 242578,
    play_addr: {
      uri: "v0200fg10000d9dq3jfog65k6jlthbug",
      url_list: [
        "https://aweme.snssdk.com/aweme/v1/playwm/?ratio=720p&video_id=v0200fg1",
      ],
    },
  },
  images: null,
};

describe("extractAwemeId", () => {
  it("覆盖站内链 / 分享链 / 图文 / 弹窗参数", () => {
    expect(extractAwemeId(`https://www.douyin.com/video/${AWEME_ID}`)).toBe(
      AWEME_ID,
    );
    expect(
      extractAwemeId(`https://www.iesdouyin.com/share/video/${AWEME_ID}/`),
    ).toBe(AWEME_ID);
    expect(
      extractAwemeId(`https://www.iesdouyin.com/share/note/${AWEME_ID}/?a=1`),
    ).toBe(AWEME_ID);
    expect(extractAwemeId(`https://www.douyin.com/slides/${AWEME_ID}`)).toBe(
      AWEME_ID,
    );
    expect(
      extractAwemeId(`https://www.douyin.com/discover?modal_id=${AWEME_ID}`),
    ).toBe(AWEME_ID);
    expect(
      extractAwemeId(`  https://www.douyin.com/video/${AWEME_ID}?x=1  `),
    ).toBe(AWEME_ID);
  });

  it("短链与非法输入取不到 ID", () => {
    expect(extractAwemeId("https://v.douyin.com/iRxAbCd/")).toBeNull();
    expect(extractAwemeId("https://www.douyin.com/user/MS4wLjABAAAA")).toBeNull();
    expect(extractAwemeId("https://www.douyin.com/video/12")).toBeNull();
    expect(extractAwemeId("not-a-url")).toBeNull();
  });
});

describe("parseDouyinRouterData", () => {
  it("解析视频作品：去水印地址、毫秒转秒、规范化来源链接", () => {
    const aweme = parseDouyinRouterData(
      routerDataHtml({ item_list: [VIDEO_ITEM], filter_list: [] }),
      AWEME_ID,
    );
    expect(aweme.awemeId).toBe(AWEME_ID);
    expect(aweme.kind).toBe("video");
    expect(aweme.title).toBe("2026企业AI落地，终极解决方案是什么？");
    // 文案未被截断时不再重复进简介
    expect(aweme.description).toBe("");
    expect(aweme.author).toBe("曲率出逃");
    expect(aweme.durationSeconds).toBe(243);
    expect(aweme.playUrl).toBe(
      "https://aweme.snssdk.com/aweme/v1/play/?ratio=720p&video_id=v0200fg1",
    );
    expect(aweme.imageMirrors).toEqual([]);
    expect(aweme.webpageUrl).toBe(`https://www.douyin.com/video/${AWEME_ID}`);
  });

  it("长文案截断为标题，完整文案留在简介", () => {
    const desc = "长".repeat(150);
    const aweme = parseDouyinRouterData(
      routerDataHtml({ item_list: [{ ...VIDEO_ITEM, desc }] }),
      AWEME_ID,
    );
    expect(aweme.title).toBe(`${"长".repeat(120)}…`);
    expect(aweme.description).toBe(desc);
  });

  it("空文案回退为带 ID 的标题", () => {
    const aweme = parseDouyinRouterData(
      routerDataHtml({ item_list: [{ ...VIDEO_ITEM, desc: "  " }] }),
      AWEME_ID,
    );
    expect(aweme.title).toBe(`抖音作品 ${AWEME_ID}`);
  });

  it("图文作品按 images 判定：抖音同样给它生成了 play_addr，不能据此判视频", () => {
    const aweme = parseDouyinRouterData(
      routerDataHtml({
        item_list: [
          {
            aweme_id: AWEME_ID,
            aweme_type: 2,
            desc: "三张图",
            author: { nickname: "作者A" },
            // 真实图文作品的形态：duration 为 0，但 play_addr 是有值的幻灯片视频
            video: {
              duration: 0,
              play_addr: {
                url_list: [
                  "https://aweme.snssdk.com/aweme/v1/playwm/?video_id=https://sf6-cdn-tos.douyinstatic.com/obj/x",
                ],
              },
            },
            images: [
              // 同一张图给了多个 CDN 镜像，全部保留供下载时降级
              {
                url_list: [
                  "https://p1.douyinpic.com/a.webp",
                  "https://p9.douyinpic.com/a.webp",
                ],
              },
              { url_list: ["https://p2.douyinpic.com/b.webp"] },
              { url_list: [] },
            ],
          },
        ],
      }),
      AWEME_ID,
    );
    expect(aweme.kind).toBe("note");
    expect(aweme.playUrl).toBeNull();
    expect(aweme.imageMirrors).toEqual([
      ["https://p1.douyinpic.com/a.webp", "https://p9.douyinpic.com/a.webp"],
      ["https://p2.douyinpic.com/b.webp"],
    ]);
    // 来源链接按实际内容走 /note/，不是按用户粘的那种形态
    expect(aweme.webpageUrl).toBe(`https://www.douyin.com/note/${AWEME_ID}`);
  });

  it("多行长文案：标题只取首行，完整文案进简介", () => {
    const desc = "我用这套方法做了一个生产级 RAG 系统。\n不用 LangChain。\n#RAG #LLM";
    const aweme = parseDouyinRouterData(
      routerDataHtml({ item_list: [{ ...VIDEO_ITEM, desc }] }),
      AWEME_ID,
    );
    expect(aweme.title).toBe("我用这套方法做了一个生产级 RAG 系统。");
    expect(aweme.title).not.toContain("\n");
    expect(aweme.description).toBe(desc);
  });

  it("作品不可用时透出平台给的原因", () => {
    expect(() =>
      parseDouyinRouterData(
        routerDataHtml({
          item_list: [],
          filter_list: [{ detail_msg: "该作品已被作者删除" }],
        }),
        AWEME_ID,
      ),
    ).toThrow("该作品已被作者删除");
  });

  it("没有 filter_list 说明时给出兜底原因", () => {
    expect(() =>
      parseDouyinRouterData(routerDataHtml({ item_list: [] }), AWEME_ID),
    ).toThrow("作品不存在或已被删除");
  });

  it("页面不含 _ROUTER_DATA → 报改版，不产出空条目", () => {
    expect(() =>
      parseDouyinRouterData("<html><body>请在抖音 App 内打开</body></html>", AWEME_ID),
    ).toThrow("改版");
  });

  it("_ROUTER_DATA 不是合法 JSON → 报解析失败", () => {
    expect(() =>
      parseDouyinRouterData(
        "<script>window._ROUTER_DATA = {loaderData: undefined};</script>",
        AWEME_ID,
      ),
    ).toThrow("解析失败");
  });
});

describe("fetchDouyinAweme", () => {
  it("链接自带 ID 时只请求一次分享页", async () => {
    const requested: string[] = [];
    const aweme = await fetchDouyinAweme(
      `https://www.douyin.com/video/${AWEME_ID}?from=feed`,
      undefined,
      {
        fetchPage: async (url) => {
          requested.push(url);
          return {
            html: routerDataHtml({ item_list: [VIDEO_ITEM] }),
            finalUrl: url,
          };
        },
      },
    );
    expect(requested).toEqual([douyinShareUrl(AWEME_ID)]);
    expect(aweme.title).toContain("2026企业AI落地");
  });

  it("note / slides 形态的链接一律走 video 分享路由（另两套取不到数据）", async () => {
    for (const source of [
      `https://www.douyin.com/slides/${AWEME_ID}`,
      `https://www.iesdouyin.com/share/note/${AWEME_ID}/`,
    ]) {
      const requested: string[] = [];
      await fetchDouyinAweme(source, undefined, {
        fetchPage: async (url) => {
          requested.push(url);
          return {
            html: routerDataHtml({ item_list: [VIDEO_ITEM] }),
            finalUrl: url,
          };
        },
      });
      expect(requested).toEqual([douyinShareUrl(AWEME_ID)]);
    }
  });

  it("短链先跟随重定向取 ID，再请求分享页", async () => {
    const requested: string[] = [];
    await fetchDouyinAweme("https://v.douyin.com/iRxAbCd/", undefined, {
      fetchPage: async (url) => {
        requested.push(url);
        return {
          html: routerDataHtml({ item_list: [VIDEO_ITEM] }),
          finalUrl: url.includes("v.douyin.com")
            ? `https://www.iesdouyin.com/share/video/${AWEME_ID}/?region=CN`
            : url,
        };
      },
    });
    expect(requested).toEqual([
      "https://v.douyin.com/iRxAbCd/",
      douyinShareUrl(AWEME_ID),
    ]);
  });

  it("重定向后仍取不到 ID → 明确报错", async () => {
    await expect(
      fetchDouyinAweme("https://v.douyin.com/iRxAbCd/", undefined, {
        fetchPage: async () => ({
          html: "",
          finalUrl: "https://www.douyin.com/",
        }),
      }),
    ).rejects.toThrow("无法从该链接解析出抖音作品 ID");
  });
});

describe("plainTextToMarkdown", () => {
  it("逐行分段：单换行在 Markdown 里只当空格，会把整篇挤成一段", () => {
    expect(plainTextToMarkdown("第一行\n第二行\n第三行")).toBe(
      "第一行\n\n第二行\n\n第三行",
    );
  });

  it("转义行首块级标记，序号与符号照原样显示", () => {
    expect(plainTextToMarkdown("1. 要点一")).toBe("1\\. 要点一");
    expect(plainTextToMarkdown("2) 要点二")).toBe("2\\) 要点二");
    expect(plainTextToMarkdown("- 列表项")).toBe("\\- 列表项");
    expect(plainTextToMarkdown("# 标题")).toBe("\\# 标题");
    expect(plainTextToMarkdown("> 引用")).toBe("\\> 引用");
    expect(plainTextToMarkdown("---")).toBe("\\---");
    expect(plainTextToMarkdown("```")).toBe("\\```");
  });

  it("行内与不构成块级标记的写法不动它", () => {
    // 抖音话题标签紧跟 # 后没有空格，本就不是标题
    expect(plainTextToMarkdown("#RAG #AI编程")).toBe("#RAG #AI编程");
    expect(plainTextToMarkdown("提升 10-15%")).toBe("提升 10-15%");
    expect(plainTextToMarkdown("见 1. 说明")).toBe("见 1. 说明");
  });

  it("空行与首尾空白归一", () => {
    expect(plainTextToMarkdown("  甲  \n\n\n  乙  ")).toBe("甲\n\n乙");
    expect(plainTextToMarkdown("   ")).toBe("");
  });
});

describe("imageExtensionFromUrl", () => {
  it("从带签名参数的图片地址取扩展名，无法识别时回退 webp", () => {
    expect(
      imageExtensionFromUrl(
        "https://p26-sign.douyinpic.com/tos-cn-i-dy/6376f0~tplv-dy:1536:1024:q80.webp?lk3s=138a59ce&x-expires=1787569200",
      ),
    ).toBe(".webp");
    expect(imageExtensionFromUrl("https://p1.douyinpic.com/a.JPEG?x=1")).toBe(
      ".jpeg",
    );
    expect(imageExtensionFromUrl("https://p1.douyinpic.com/noext")).toBe(
      ".webp",
    );
    expect(imageExtensionFromUrl("not-a-url")).toBe(".webp");
  });
});

describe("douyinImageNoteSource", () => {
  it("元数据行带上图片张数；标题只是文案首行，要留给 AI 重拟", () => {
    const source = douyinImageNoteSource({
      awemeId: AWEME_ID,
      kind: "note",
      title: "三张图",
      description: "",
      author: "作者A",
      durationSeconds: null,
      playUrl: null,
      imageMirrors: [["a"], ["b"], ["c"]],
      webpageUrl: `https://www.douyin.com/note/${AWEME_ID}`,
    });
    expect(buildImageNoteMetaLine(source)).toBe(
      "平台：抖音 · 作者：作者A · 图文 3 张",
    );
    expect(source.authoredTitle).toBe(false);
  });
});
