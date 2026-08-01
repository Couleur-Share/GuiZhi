import { describe, expect, it, vi } from "vitest";
import { detectForumPlatform } from "@guizhi/shared/utils/forum-platforms";

vi.mock("electron", () => ({
  session: { defaultSession: {} },
  app: {},
}));

import {
  escapeControlCharsInJsonStrings,
  extractGuestJs,
  extractNgaReplyContext,
  fetchNgaThread,
  mergeNgaCookies,
  ngaBbcodeToMarkdown,
  parseNgaStore,
  resolveNgaImageUrl,
  NGA_IMAGE_LIMIT,
} from "../../../src/main/services/import/nga";
import { extractForumPost } from "../../../src/main/services/import/forum-post";
import type { FetchRawTextResult } from "../../../src/main/services/import/safe-fetch";

function wrapStore(data: unknown, extra: Record<string, unknown> = {}): string {
  return `window.script_muti_get_var_store=${JSON.stringify({
    data,
    encode: "utf-8",
    ...extra,
  })}`;
}

function pagePayload(overrides: {
  page?: number;
  rows?: number;
  replies?: NgaReplyDraft[];
  opContent?: string;
  attachs?: Record<string, { attachurl: string; type: string }>;
} = {}) {
  const page = overrides.page ?? 1;
  const replies = overrides.replies ?? [
    { lou: 1, author: "楼一", content: "第一页回复", ts: 1690703400, authorid: 2 },
  ];
  const r: Record<string, unknown> = {
    "0": {
      lou: 0,
      author: "楼主",
      authorid: 1,
      content: overrides.opContent ?? "主楼正文[img]./mon_2023/a.jpg[/img]",
      postdatetimestamp: 1690703293,
      subject: "测试标题",
      attachs: overrides.attachs ?? {
        "0": { attachurl: "mon_2023/a.jpg", type: "img" },
      },
    },
  };
  for (const [index, reply] of replies.entries()) {
    r[String(index + 1)] = {
      lou: reply.lou,
      author: reply.author,
      authorid: reply.authorid ?? 2,
      content: reply.content,
      postdatetimestamp: reply.ts,
    };
  }
  return {
    __GLOBAL: { _ATTACH_BASE_VIEW: "img.nga.cn/attachments" },
    __T: {
      tid: 37194262,
      fid: 570,
      subject: "测试标题",
      author: "楼主",
      authorid: 1,
      replies: overrides.rows ? overrides.rows - 1 : 1,
      postdate: 1690703293,
    },
    __R: r,
    __U: {
      "1": { uid: 1, username: "楼主" },
      "2": { uid: 2, username: "楼一" },
    },
    __F: { "570": { name: "硬件数码" } },
    __ROWS: overrides.rows ?? 2,
    __PAGE: page,
  };
}

interface NgaReplyDraft {
  lou: number;
  author: string;
  content: string;
  ts: number;
  authorid?: number;
}

/** 模拟 NGA 的 authorid 过滤：只留下该作者的楼 */
function filterPayloadByAuthor(
  payload: ReturnType<typeof pagePayload>,
  authorId: number | undefined,
): ReturnType<typeof pagePayload> {
  if (authorId == null) {
    return payload;
  }
  const filtered: Record<string, unknown> = {};
  let index = 0;
  for (const value of Object.values(payload.__R)) {
    const post = value as { authorid?: number };
    if (post.authorid === authorId) {
      filtered[String(index)] = value;
      index += 1;
    }
  }
  return {
    ...payload,
    __R: filtered,
    __ROWS: index,
  };
}

function authorIdFromUrl(url: string): number | undefined {
  const match = /[?&]authorid=(-?\d+)/.exec(url);
  return match ? Number(match[1]) : undefined;
}

function pageFromUrl(url: string): number {
  const match = /[?&]page=(\d+)/.exec(url);
  return match ? Number(match[1]) : 1;
}

function textResult(
  text: string,
  status = 200,
  setCookies: string[] = [],
): FetchRawTextResult {
  return {
    status,
    text,
    contentType: "text/plain; charset=utf-8",
    finalUrl: "https://bbs.nga.cn/read.php",
    setCookies,
  };
}

describe("detectForumPlatform · NGA", () => {
  it("识别 bbs / ngabbs / 178 域名的 tid 链接", () => {
    expect(
      detectForumPlatform(
        "https://bbs.nga.cn/read.php?tid=37194262&fav=:FE2BE56B0&rand=862",
      ),
    ).toEqual({ platform: "nga", topicId: "37194262" });
    expect(
      detectForumPlatform("https://ngabbs.com/read.php?tid=123"),
    ).toEqual({ platform: "nga", topicId: "123" });
    expect(
      detectForumPlatform("https://nga.178.com/read.php?tid=99"),
    ).toEqual({ platform: "nga", topicId: "99" });
  });

  it("没有 tid 或非 read.php 不认", () => {
    expect(
      detectForumPlatform("https://bbs.nga.cn/thread.php?fid=570"),
    ).toBeNull();
    expect(
      detectForumPlatform("https://bbs.nga.cn/read.php?pid=1"),
    ).toBeNull();
  });
});

describe("NGA 解析工具", () => {
  it("extractGuestJs 从挑战正文抠值", () => {
    expect(
      extractGuestJs("document.cookie = 'guestJs=1785489313_1eb3xt6;domain='"),
    ).toBe("1785489313_1eb3xt6");
    expect(extractGuestJs("no cookie here")).toBeNull();
  });

  it("parseNgaStore 剥前缀并容忍尾部分号", () => {
    const store = parseNgaStore(
      `${wrapStore({ __ROWS: 1 })};`,
    );
    expect(store.data?.__ROWS).toBe(1);
  });

  it("parseNgaStore 容忍字符串里的未转义 tab", () => {
    // 真实 NGA 响应里用户备注常用 tab 分隔字段，严格 JSON 不允许
    const raw =
      'window.script_muti_get_var_store={"data":{"__U":{"1":{"username":"a","remark":{"0":{"4":"sv\twow"}}}},"__ROWS":1},"encode":"gbk"}';
    const store = parseNgaStore(raw);
    expect(store.data?.__ROWS).toBe(1);
    expect(
      (store.data?.__U as Record<string, { remark?: { "0"?: { "4"?: string } } }>)
        ?.["1"]?.remark?.["0"]?.["4"],
    ).toBe("sv\twow");
  });

  it("escapeControlCharsInJsonStrings 只改字符串内部", () => {
    expect(escapeControlCharsInJsonStrings('{"a":"x\ty"}')).toBe(
      '{"a":"x\\ty"}',
    );
  });

  it("mergeNgaCookies 合并 Set-Cookie 与 body 里的 guestJs", () => {
    expect(
      mergeNgaCookies(undefined, ["ngaPassportUid=guest1"], "guestJs=abc_1;"),
    ).toBe("ngaPassportUid=guest1; guestJs=abc_1");
  });

  it("resolveNgaImageUrl 拼相对路径", () => {
    expect(
      resolveNgaImageUrl("./mon_2023/a.jpg", "https://img.nga.cn/attachments"),
    ).toBe("https://img.nga.cn/attachments/mon_2023/a.jpg");
    expect(
      resolveNgaImageUrl(
        "https://img.nga.cn/attachments/x.jpg",
        "https://img.nga.cn/attachments",
      ),
    ).toBe("https://img.nga.cn/attachments/x.jpg");
  });

  it("ngaBbcodeToMarkdown 转换常见标签", () => {
    const md = ngaBbcodeToMarkdown(
      "[b]粗[/b]<br/>[quote]引用行[/quote]<br/>[url=https://a.com]链[/url]<br/>[img]./p.jpg[/img]",
      {
        attachBase: "https://img.nga.cn/attachments",
        imageMap: new Map([
          [
            "https://img.nga.cn/attachments/p.jpg",
            "local-image://import-abcd.jpg",
          ],
        ]),
      },
    );
    expect(md).toContain("<strong>粗</strong>");
    expect(md).toContain("> 引用行");
    expect(md).toContain("[链](https://a.com)");
    expect(md).toContain("![图](local-image://import-abcd.jpg)");
  });

  it("居中粗体短标题收成二级标题，不留下失效的 **", () => {
    const md = ngaBbcodeToMarkdown(
      "[align=center][size=150%][b]\n配镜过程分享\n[/b][/size][/align]\n这块主要是分享",
    );
    expect(md).toContain("## 配镜过程分享");
    expect(md).not.toMatch(/\*\*\s*配镜过程分享/);
    expect(md).toContain("这块主要是分享");
  });

  it("[h] 标题与空分隔线", () => {
    expect(ngaBbcodeToMarkdown("[h]前言[/h]正文")).toContain("## 前言");
    expect(ngaBbcodeToMarkdown("[h][/h]")).toContain("---");
  });

  it("粗体内换行会 trim；中文夹缝用 strong 而非 **", () => {
    expect(ngaBbcodeToMarkdown("[b]\n前言\n[/b]")).toBe("<strong>前言</strong>");
    expect(
      ngaBbcodeToMarkdown(
        "可以说[b]挑选镜框是和验光同等重要的步骤。[/b]实践中",
      ),
    ).toBe(
      "可以说<strong>挑选镜框是和验光同等重要的步骤。</strong>实践中",
    );
  });

  it("size 包在粗体内的短标题收成 ##，不留下带空格的 **", () => {
    const md = ngaBbcodeToMarkdown(
      "[b][size=150%] 配镜过程分享 [/size][/b]\n正文一段",
    );
    expect(md).toContain("## 配镜过程分享");
    expect(md).not.toMatch(/\*\*\s*配镜过程分享/);
  });

  it("collapse 产出 details，color 产出白名单 class", () => {
    const md = ngaBbcodeToMarkdown(
      "[collapse=有些长折叠了]隐藏内容[/collapse]\n[color=red]强烈建议[/color]",
    );
    expect(md).toContain("<details>");
    expect(md).toContain("<summary>有些长折叠了</summary>");
    expect(md).toContain("隐藏内容");
    expect(md).toContain('<span class="forum-color-red">强烈建议</span>');
  });

  it("extractNgaReplyContext 抽出引用头并剥掉首条 quote", () => {
    const raw =
      "[quote][pid=123,1,1]Reply[/pid] [b]Post by [uid=9]lyzlegend[/uid] (2023-07-30 16:35):[/b]\n对方原话在这里比较长[/quote]\n楼主回答";
    const ctx = extractNgaReplyContext(raw);
    expect(ctx.replyTo?.author).toBe("lyzlegend");
    expect(ctx.replyTo?.pid).toBe(123);
    expect(ctx.replyTo?.snippet).toContain("对方原话");
    expect(ctx.content).toBe("楼主回答");
  });

  it("extractNgaReplyContext 摘要不留下字面量 br", () => {
    const raw =
      "[quote][pid=1,1,1]Reply[/pid] [b]Post by [uid=1]牧云吹雪[/uid] (2024-01-01 12:00):[/b]<br/><br/>想看看楼主整个眼镜是什么样的[/quote]\n楼主答";
    const ctx = extractNgaReplyContext(raw);
    expect(ctx.replyTo?.author).toBe("牧云吹雪");
    expect(ctx.replyTo?.snippet).toBe("想看看楼主整个眼镜是什么样的");
    expect(ctx.replyTo?.snippet).not.toMatch(/<br/i);
  });
});

describe("fetchNgaThread", () => {
  it("guestJs 握手后拉帖并替换附件图；讨论区不收录他人回复", async () => {
    const calls: string[] = [];
    const thread = await fetchNgaThread(
      "37194262",
      {
        retryDelaysMs: [],
        fetchRawText: async (url, options) => {
          calls.push(url);
          if (!options?.cookie || !options.cookie.includes("guestJs=")) {
            return textResult(
              `window.script_muti_get_var_store={"error":{"0":"15:访客不能直接访问","1":"guestJs=abc_guest;"},"data":{"__MESSAGE":{"0":15,"1":"访客不能直接访问","3":403}}}`,
              403,
              ["ngaPassportUid=guest-test", "lastvisit=1"],
            );
          }
          expect(options.cookie).toContain("guestJs=abc_guest");
          expect(options.cookie).toContain("ngaPassportUid=guest-test");
          const payload = filterPayloadByAuthor(
            pagePayload(),
            authorIdFromUrl(url),
          );
          return textResult(wrapStore(payload));
        },
        downloadImage: async () => ({
          dir: "C:\\tmp\\nga-img",
          filePath: "C:\\tmp\\nga-img\\image.jpg",
        }),
        saveImageAsset: async () => "import-deadbeef012345.jpg",
      },
    );

    expect(thread.platform).toBe("nga");
    expect(thread.title).toBe("测试标题");
    expect(thread.author).toBe("楼主");
    expect(thread.node).toBe("硬件数码");
    expect(thread.webpageUrl).toBe(
      "https://bbs.nga.cn/read.php?tid=37194262",
    );
    expect(thread.content).toContain(
      "![图](local-image://import-deadbeef012345.jpg)",
    );
    expect(thread.replyRetention).toBe("op-only");
    expect(thread.replies).toHaveLength(0);
    expect(thread.summaryReplies).toHaveLength(1);
    expect(thread.summaryReplies?.[0]?.floor).toBe(1);
    expect(calls.some((url) => url.includes("page=1"))).toBe(true);
    expect(calls.some((url) => url.includes("authorid=1"))).toBe(true);
  });

  it("采样他人回复供总结，入库只留楼主回复", async () => {
    const thread = await fetchNgaThread(
      "37194262",
      {
        retryDelaysMs: [],
        fetchRawText: async (url) => {
          const authorId = authorIdFromUrl(url);
          const page = pageFromUrl(url);
          if (authorId === 1) {
            return textResult(
              wrapStore(
                filterPayloadByAuthor(
                  pagePayload({
                    page: 1,
                    rows: 22,
                    replies: [
                      {
                        lou: 5,
                        author: "楼主",
                        content: "楼主补充",
                        ts: 1690703600,
                        authorid: 1,
                      },
                    ],
                  }),
                  1,
                ),
              ),
            );
          }
          if (page === 1) {
            return textResult(
              wrapStore(
                pagePayload({
                  page: 1,
                  rows: 22,
                  replies: [
                    {
                      lou: 1,
                      author: "A",
                      content: "p1",
                      ts: 1690703400,
                      authorid: 2,
                    },
                  ],
                }),
              ),
            );
          }
          return textResult(
            wrapStore(
              pagePayload({
                page: 2,
                rows: 22,
                opContent: "不应覆盖主楼",
                replies: [
                  {
                    lou: 20,
                    author: "B",
                    content: "p2",
                    ts: 1690703500,
                    authorid: 2,
                  },
                ],
              }),
            ),
          );
        },
        downloadImage: async () => {
          throw new Error("skip images");
        },
      },
    );

    expect(thread.replies.map((r) => r.floor)).toEqual([5]);
    expect(thread.replies[0]?.content).toContain("楼主补充");
    expect(
      thread.summaryReplies?.map((r) => r.floor).sort((a, b) => a - b),
    ).toEqual([1, 20]);
    expect(thread.content).toContain("主楼正文");
    expect(thread.warningReason).toMatch(/下载失败|外链/);
  });

  it("超长帖只采样有限页并写清 warning", async () => {
    const pagesRequested = new Set<number>();
    const thread = await fetchNgaThread(
      "37194262",
      {
        retryDelaysMs: [],
        maxSummaryPages: 2,
        fetchRawText: async (url) => {
          const authorId = authorIdFromUrl(url);
          const page = pageFromUrl(url);
          if (authorId == null) {
            pagesRequested.add(page);
          }
          return textResult(
            wrapStore(
              filterPayloadByAuthor(
                pagePayload({
                  page,
                  rows: 200,
                  replies:
                    page === 1
                      ? [
                          {
                            lou: 1,
                            author: "A",
                            content: "early",
                            ts: 1,
                            authorid: 2,
                          },
                        ]
                      : [
                          {
                            lou: page * 10,
                            author: "B",
                            content: `p${page}`,
                            ts: page,
                            authorid: 2,
                          },
                        ],
                }),
                authorId,
              ),
            ),
          );
        },
        downloadImage: async () => {
          throw new Error("skip");
        },
      },
    );

    expect([...pagesRequested].sort((a, b) => a - b)).toEqual([1, 2]);
    expect(thread.warningReason).toMatch(/仅采样前 2 页/);
    expect(
      thread.summaryReplies?.map((r) => r.floor).sort((a, b) => a - b),
    ).toEqual([1, 20]);
  });

  it("附件图超过上限时保留外链并 warning", async () => {
    const attachs: Record<string, { attachurl: string; type: string }> = {};
    for (let i = 0; i < 3; i++) {
      attachs[String(i)] = {
        attachurl: `mon_2023/${i}.jpg`,
        type: "img",
      };
    }
    const thread = await fetchNgaThread(
      "37194262",
      {
        retryDelaysMs: [],
        imageLimit: 1,
        fetchRawText: async (url) =>
          textResult(
            wrapStore(
              filterPayloadByAuthor(
                pagePayload({
                  opContent: "只有附件",
                  attachs,
                  replies: [],
                  rows: 1,
                }),
                authorIdFromUrl(url),
              ),
            ),
          ),
        downloadImage: async () => ({
          dir: "C:\\tmp\\nga-img",
          filePath: "C:\\tmp\\nga-img\\image.jpg",
        }),
        saveImageAsset: async () => "import-one.jpg",
      },
    );

    expect(thread.content).toContain("local-image://import-one.jpg");
    expect(thread.content).toContain("https://img.nga.cn/attachments/");
    expect(thread.warningReason).toMatch(/超过 1 张上限/);
    expect(NGA_IMAGE_LIMIT).toBe(80);
  });

  it("访客无法访问时给出可读错误", async () => {
    await expect(
      fetchNgaThread("1", {
        retryDelaysMs: [],
        fetchRawText: async () =>
          textResult(
            `window.script_muti_get_var_store={"error":{"0":"15:访客不能直接访问","1":"no guest"},"data":{}}`,
            403,
          ),
      }),
    ).rejects.toThrow(/登录/);
  });
});

describe("extractForumPost · NGA", () => {
  it("把 warningReason 带到 ExtractedContent，元数据标明楼主保留策略", async () => {
    const result = await extractForumPost(
      { platform: "nga", topicId: "1" },
      {
        fetchThread: async () => ({
          platform: "nga",
          topicId: "1",
          title: "t",
          author: "a",
          node: "n",
          createdAt: Date.now(),
          replyCount: 2040,
          content: "body",
          replies: [
            {
              floor: 12,
              author: "a",
              content: "楼主回",
              createdAt: Date.now(),
            },
          ],
          summaryReplies: [
            {
              floor: 1,
              author: "他人",
              content: "提问",
              createdAt: Date.now(),
            },
          ],
          replyRetention: "op-only",
          webpageUrl: "https://bbs.nga.cn/read.php?tid=1",
          warningReason: "2 张附件图下载失败，已保留外链",
        }),
        getSummaryConfig: () => null,
      },
    );
    expect(result.itemType).toBe("forum");
    expect(result.warningReason).toBe("2 张附件图下载失败，已保留外链");
    expect(result.content).toContain("平台：NGA");
    expect(result.content).toContain(
      "2040 条回复（入库保留楼主 1 条）",
    );
    expect(result.content).toContain(
      "## 讨论（楼主 1 条 · 原帖共 2040 条）",
    );
    expect(result.content).toContain("### 12 楼 · a");
    expect(result.content).toContain("仅保留楼主回复");
    expect(result.content).not.toContain("原始讨论已完整入库");
  });
});
