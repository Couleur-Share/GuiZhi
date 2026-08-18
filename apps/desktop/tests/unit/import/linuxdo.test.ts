import { describe, expect, it, vi } from "vitest";
import {
  detectForumPlatform,
  linuxdoCanonicalUrl,
} from "@guizhi/shared/utils/forum-platforms";
import { splitForumNoteSections } from "@guizhi/shared/utils/forum-note";
import { parseVideoMetaBlock } from "@guizhi/shared/utils/video-meta";

vi.mock("electron", () => ({
  session: { defaultSession: {} },
  app: {},
}));

import { fetchLinuxdoThread } from "../../../src/main/services/import/linuxdo";
import { extractForumPost } from "../../../src/main/services/import/forum-post";

function topicPayload(overrides: Record<string, unknown> = {}) {
  return {
    id: 2702071,
    title: "如何在 Linux 上部署 Agent？",
    posts_count: 4,
    created_at: "2024-06-01T08:00:00.000Z",
    tags: ["开发调优"],
    details: {
      created_by: { username: "op_user", name: "楼主" },
      category: { name: "开发调优" },
    },
    post_stream: {
      stream: [101, 102, 103, 104],
      posts: [
        {
          id: 101,
          username: "op_user",
          post_number: 1,
          created_at: "2024-06-01T08:00:00.000Z",
          raw: "主楼正文\n\n```bash\nnpm install\n```",
        },
        {
          id: 102,
          username: "alice",
          post_number: 2,
          created_at: "2024-06-01T08:05:00.000Z",
          raw: "感谢分享",
          reply_to_post_number: 1,
          actions_summary: [{ id: 2, count: 4 }],
        },
        {
          id: 103,
          username: "op_user",
          post_number: 3,
          created_at: "2024-06-01T08:10:00.000Z",
          raw: "补充文档链接",
          reply_to_post_number: 2,
          actions_summary: [{ id: 2, count: 10 }],
        },
        {
          id: 104,
          username: "bob",
          post_number: 4,
          created_at: "2024-06-01T08:15:00.000Z",
          cooked: "<p>HTML 降级正文</p>",
          actions_summary: [{ id: 2, count: 2 }],
        },
      ],
    },
    ...overrides,
  };
}

describe("detectForumPlatform · LINUX DO", () => {
  it("识别常见 URL 形态", () => {
    expect(detectForumPlatform("https://linux.do/t/topic/2702071")).toEqual({
      platform: "linuxdo",
      topicId: "2702071",
    });
    expect(detectForumPlatform("https://linux.do/t/2702071")).toEqual({
      platform: "linuxdo",
      topicId: "2702071",
    });
    expect(detectForumPlatform("https://linux.do/t/some-slug/2702071")).toEqual(
      {
        platform: "linuxdo",
        topicId: "2702071",
      },
    );
    expect(detectForumPlatform("https://linux.do/t/slug/2702071/5")).toEqual({
      platform: "linuxdo",
      topicId: "2702071",
    });
  });

  it("非帖子路径返回 null", () => {
    expect(detectForumPlatform("https://linux.do/c/dev")).toBeNull();
    expect(detectForumPlatform("https://example.com/t/2702071")).toBeNull();
  });

  it("规范链接用于去重", () => {
    expect(linuxdoCanonicalUrl("2702071")).toBe(
      "https://linux.do/t/topic/2702071",
    );
  });
});

describe("fetchLinuxdoThread", () => {
  it("解析 raw 正文并保留所有参与者的完整讨论", async () => {
    const fetchJson = vi.fn().mockResolvedValue(topicPayload());
    const thread = await fetchLinuxdoThread("2702071", { fetchJson });

    expect(thread.platform).toBe("linuxdo");
    expect(thread.title).toBe("如何在 Linux 上部署 Agent？");
    expect(thread.author).toBe("楼主");
    expect(thread.node).toBe("开发调优");
    expect(thread.content).toContain("npm install");
    expect(thread.replyRetention).toBe("all");
    expect(thread.replies.map((reply) => reply.floor)).toEqual([2, 3, 4]);
    expect(thread.replies.map((reply) => reply.author)).toEqual([
      "alice",
      "op_user",
      "bob",
    ]);
    expect(thread.summaryReplies).toBeUndefined();
    expect(thread.webpageUrl).toBe("https://linux.do/t/topic/2702071");
  });

  it("cooked 字段可降级为 Markdown", async () => {
    const fetchJson = vi.fn().mockResolvedValue(
      topicPayload({
        post_stream: {
          stream: [101, 104],
          posts: [
            {
              id: 101,
              username: "op_user",
              post_number: 1,
              raw: "主楼",
            },
            {
              id: 104,
              username: "bob",
              post_number: 4,
              cooked: "<p><strong>粗体</strong></p>",
            },
          ],
        },
        posts_count: 2,
      }),
    );
    const thread = await fetchLinuxdoThread("2702071", { fetchJson });
    expect(thread.replies[0]?.content).toContain("粗体");
  });

  it("403 时降级到 Electron 会话", async () => {
    const fetchJson = vi
      .fn()
      .mockRejectedValue(new Error("HTTP 403: Forbidden"));
    const fetchAuthenticatedJson = vi.fn().mockResolvedValue(topicPayload());
    await fetchLinuxdoThread("2702071", { fetchJson, fetchAuthenticatedJson });
    expect(fetchAuthenticatedJson).toHaveBeenCalledWith(
      "https://linux.do/t/topic/2702071.json",
      undefined,
    );
  });

  it("403 且无会话时给出可读错误", async () => {
    await expect(
      fetchLinuxdoThread("2702071", {
        fetchJson: vi.fn().mockRejectedValue(new Error("HTTP 403")),
      }),
    ).rejects.toThrow(/Cloudflare/);
  });

  it("404 时给出可读错误", async () => {
    await expect(
      fetchLinuxdoThread("2702071", {
        fetchJson: vi.fn().mockRejectedValue(new Error("HTTP 404")),
      }),
    ).rejects.toThrow(/不存在/);
  });

  it("reply_to_post_number 映射为引用上下文", async () => {
    const fetchJson = vi.fn().mockResolvedValue(topicPayload());
    const thread = await fetchLinuxdoThread("2702071", { fetchJson });
    const opReply = thread.replies.find((reply) => reply.floor === 3);
    expect(opReply?.replyTo?.floor).toBe(2);
    expect(opReply?.replyTo?.author).toBe("alice");
    expect(opReply?.replyTo?.snippet).toContain("感谢分享");
  });

  it("长帖不按作者或前若干楼截断讨论素材", async () => {
    const posts = Array.from({ length: 50 }, (_, index) => ({
      id: 200 + index,
      username: index % 5 === 0 ? "op_user" : `user${index}`,
      post_number: index + 1,
      raw: `回复 ${index + 1}`,
    }));
    const fetchJson = vi.fn().mockResolvedValue(
      topicPayload({
        posts_count: posts.length,
        post_stream: {
          stream: posts.map((p) => p.id),
          posts,
        },
      }),
    );
    const thread = await fetchLinuxdoThread("2702071", { fetchJson });
    expect(thread.replies).toHaveLength(49);
    expect(thread.replies.at(-1)?.floor).toBe(50);
  });
});

describe("extractForumPost · LINUX DO", () => {
  it("组装 forum 条目并写入元数据引用块", async () => {
    const entry = await extractForumPost(
      { platform: "linuxdo", topicId: "2702071" },
      {
        fetchThread: async () => ({
          platform: "linuxdo",
          topicId: "2702071",
          title: "测试帖",
          author: "楼主",
          node: "开发调优",
          createdAt: Date.parse("2024-06-01T08:00:00.000Z"),
          replyCount: 2,
          content: "主楼内容",
          replies: [
            {
              floor: 2,
              author: "alice",
              content: "另一位用户的方案",
              createdAt: Date.parse("2024-06-01T08:30:00.000Z"),
            },
            {
              floor: 3,
              author: "楼主",
              content: "跟进",
              createdAt: Date.parse("2024-06-01T09:00:00.000Z"),
            },
          ],
          replyRetention: "all",
          webpageUrl: linuxdoCanonicalUrl("2702071"),
        }),
        getSummaryConfig: () => null,
      },
    );

    expect(entry.itemType).toBe("forum");
    expect(entry.sourceUri).toBe("https://linux.do/t/topic/2702071");
    const meta = parseVideoMetaBlock(entry.content);
    expect(meta?.platform).toBe("LINUX DO");
    const sections = splitForumNoteSections(entry.content);
    expect(sections.body).toContain("主楼内容");
    expect(sections.replies).toContain("另一位用户的方案");
    expect(sections.replies).toContain("跟进");
  });
});
