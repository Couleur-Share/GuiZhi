import { describe, expect, it, vi } from "vitest";
import { detectForumPlatform } from "@guizhi/shared/utils/forum-platforms";

vi.mock("electron", () => ({
  session: { defaultSession: {} },
  app: {},
}));

import {
  escapeControlCharsInJsonStrings,
  extractGuestJs,
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
    { lou: 1, author: "楼一", content: "第一页回复", ts: 1690703400 },
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
      authorid: 2,
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
    expect(md).toContain("**粗**");
    expect(md).toContain("> 引用行");
    expect(md).toContain("[链](https://a.com)");
    expect(md).toContain("![图](local-image://import-abcd.jpg)");
  });
});

describe("fetchNgaThread", () => {
  it("guestJs 握手后拉帖并替换附件图", async () => {
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
          return textResult(wrapStore(pagePayload()));
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
    expect(thread.replies).toHaveLength(1);
    expect(thread.replies[0]?.floor).toBe(1);
    expect(calls.some((url) => url.includes("page=1"))).toBe(true);
  });

  it("分页合并多页回复", async () => {
    const thread = await fetchNgaThread(
      "37194262",
      {
        retryDelaysMs: [],
        fetchRawText: async (url) => {
          if (url.includes("page=1")) {
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

    expect(thread.replies.map((r) => r.floor).sort((a, b) => a - b)).toEqual([
      1, 20,
    ]);
    expect(thread.content).toContain("主楼正文");
    expect(thread.warningReason).toMatch(/下载失败|外链/);
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
        fetchRawText: async () =>
          textResult(
            wrapStore(
              pagePayload({
                opContent: "只有附件",
                attachs,
                replies: [],
                rows: 1,
              }),
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
  it("把 warningReason 带到 ExtractedContent", async () => {
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
          replyCount: 0,
          content: "body",
          replies: [],
          webpageUrl: "https://bbs.nga.cn/read.php?tid=1",
          warningReason: "2 张附件图下载失败，已保留外链",
        }),
        getSummaryConfig: () => null,
      },
    );
    expect(result.itemType).toBe("forum");
    expect(result.warningReason).toBe("2 张附件图下载失败，已保留外链");
    expect(result.content).toContain("平台：NGA");
  });
});
