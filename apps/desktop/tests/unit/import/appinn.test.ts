import { describe, expect, it, vi } from "vitest";
import {
  appinnCanonicalUrl,
  detectForumPlatform,
} from "@guizhi/shared/utils/forum-platforms";
import { splitForumNoteSections } from "@guizhi/shared/utils/forum-note";
import { parseVideoMetaBlock } from "@guizhi/shared/utils/video-meta";

vi.mock("electron", () => ({
  session: { defaultSession: {} },
  app: {},
}));

import { fetchAppinnThread } from "../../../src/main/services/import/appinn";
import { extractForumPost } from "../../../src/main/services/import/forum-post";

function topicPayload() {
  return {
    id: 89533,
    title: "PDE5 抑制剂这么猛？",
    posts_count: 3,
    created_at: "2026-08-08T06:34:50.807Z",
    category_id: 5,
    tags: [],
    details: {
      created_by: { username: "Qingwa", name: "青小蛙" },
    },
    post_stream: {
      stream: [337122, 337123, 337124],
      posts: [
        {
          id: 337122,
          username: "Qingwa",
          name: "青小蛙",
          post_number: 1,
          created_at: "2026-08-08T06:34:51.067Z",
          cooked:
            '<p><a href="https://weibo.com/example">来源链接</a></p><p><strong>主楼正文</strong></p>',
        },
        {
          id: 337123,
          username: "alice",
          post_number: 2,
          created_at: "2026-08-08T07:00:00.000Z",
          reply_to_post_number: 1,
          cooked: "<p>匿名接口只有 cooked，也要完整保留。</p>",
        },
        {
          id: 337124,
          username: "bob",
          post_number: 3,
          created_at: "2026-08-08T08:00:00.000Z",
          cooked: "<p>第三楼</p>",
        },
      ],
    },
  };
}

describe("detectForumPlatform · 小众软件", () => {
  it("识别 Discourse 常见 URL 形态", () => {
    expect(
      detectForumPlatform("https://meta.appinn.net/t/topic/89533"),
    ).toEqual({ platform: "appinn", topicId: "89533" });
    expect(
      detectForumPlatform("https://meta.appinn.net/t/pde5/89533/3?u=reader"),
    ).toEqual({ platform: "appinn", topicId: "89533" });
  });

  it("只认官方论坛主机并生成稳定规范链接", () => {
    expect(
      detectForumPlatform("https://meta.appinn.net.example.com/t/topic/89533"),
    ).toBeNull();
    expect(appinnCanonicalUrl("89533")).toBe(
      "https://meta.appinn.net/t/topic/89533",
    );
  });
});

describe("fetchAppinnThread", () => {
  it("按真实匿名响应形状把 cooked 转为 Markdown，并从 site.json 解析板块", async () => {
    const fetchJson = vi.fn().mockImplementation(async (url: string) => {
      if (url === "https://meta.appinn.net/site.json") {
        return { categories: [{ id: 5, name: "闲聊灌水" }] };
      }
      return topicPayload();
    });

    const thread = await fetchAppinnThread("89533", { fetchJson });

    expect(thread.platform).toBe("appinn");
    expect(thread.title).toBe("PDE5 抑制剂这么猛？");
    expect(thread.author).toBe("青小蛙");
    expect(thread.node).toBe("闲聊灌水");
    expect(thread.content).toContain("[来源链接](https://weibo.com/example)");
    expect(thread.content).toContain("**主楼正文**");
    expect(thread.replies.map((reply) => reply.floor)).toEqual([2, 3]);
    expect(thread.replies[0]?.content).toContain("只有 cooked");
    expect(thread.replies[0]?.replyTo?.author).toBe("Qingwa");
    expect(thread.webpageUrl).toBe(
      "https://meta.appinn.net/t/topic/89533",
    );
    expect(fetchJson).toHaveBeenCalledWith(
      "https://meta.appinn.net/site.json",
      undefined,
    );
  });
});

describe("extractForumPost · 小众软件", () => {
  it("组装 forum 条目、平台元数据与逐楼讨论", async () => {
    const entry = await extractForumPost(
      { platform: "appinn", topicId: "89533" },
      {
        fetchThread: async () => ({
          platform: "appinn",
          topicId: "89533",
          title: "测试帖",
          author: "青小蛙",
          node: "闲聊灌水",
          createdAt: Date.parse("2026-08-08T06:34:50.807Z"),
          replyCount: 1,
          content: "主楼内容",
          replies: [
            {
              floor: 2,
              author: "alice",
              content: "讨论内容",
              createdAt: Date.parse("2026-08-08T07:00:00.000Z"),
            },
          ],
          replyRetention: "all",
          webpageUrl: appinnCanonicalUrl("89533"),
        }),
        getSummaryConfig: () => null,
      },
    );

    expect(entry.itemType).toBe("forum");
    expect(entry.sourceUri).toBe("https://meta.appinn.net/t/topic/89533");
    expect(parseVideoMetaBlock(entry.content)?.platform).toBe("小众软件");
    const sections = splitForumNoteSections(entry.content);
    expect(sections.body).toContain("主楼内容");
    expect(sections.replies).toContain("讨论内容");
  });
});
