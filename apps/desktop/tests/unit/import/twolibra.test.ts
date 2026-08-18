import { describe, expect, it, vi } from "vitest";
import {
  detectForumPlatform,
  twolibraCanonicalUrl,
} from "@guizhi/shared/utils/forum-platforms";
import { splitForumNoteSections } from "@guizhi/shared/utils/forum-note";
import { parseVideoMetaBlock } from "@guizhi/shared/utils/video-meta";

vi.mock("electron", () => ({
  session: { defaultSession: {} },
  app: {},
}));

import { extractForumPost } from "../../../src/main/services/import/forum-post";
import { fetchTwolibraThread } from "../../../src/main/services/import/twolibra";

const TOPIC_ID = "bUSaOUc";

function postPayload() {
  return {
    c: 0,
    m: "请求成功",
    d: {
      short_id: TOPIC_ID,
      title: "【💰】记一下验光体验",
      content: "主楼第一段\n\n- 建议一\n- 建议二",
      author: { username: "z1gui" },
      node: { name: "健康咨询", slug: "health-consultation" },
      created_at: "2026-06-24T06:18:54.029Z",
      comment_count: 3,
    },
  };
}

function commentPayload(page: number) {
  return {
    c: 0,
    m: "请求成功",
    d: {
      total: 3,
      page,
      limit: 2,
      total_pages: 2,
      items:
        page === 1
          ? [
              {
                id: "comment-1",
                content: "第一条评论",
                author: { username: "alice" },
                floor: 1,
                flat_floor: 1,
                created_at: "2026-06-24T06:24:06.183Z",
              },
              {
                id: "comment-2",
                content: "回复第一条",
                author: { username: "bob" },
                floor: null,
                flat_floor: 2,
                reply_comment: {
                  content: "第一条评论",
                  author: { username: "alice" },
                  floor: 1,
                  flat_floor: 1,
                },
                created_at: "2026-06-24T06:25:00.000Z",
              },
            ]
          : [
              {
                id: "comment-3",
                content: "已删除内容不应入库",
                author: { username: "charlie" },
                floor: 2,
                flat_floor: 3,
                is_deleted: true,
                created_at: "2026-06-24T06:26:00.000Z",
              },
            ],
    },
  };
}

describe("detectForumPlatform · 2Libra", () => {
  it("只识别官方帖子短链形态", () => {
    expect(
      detectForumPlatform(
        "https://2libra.com/post/health-consultation/bUSaOUc?commentId=x&p=2",
      ),
    ).toEqual({ platform: "twolibra", topicId: TOPIC_ID });
    expect(
      detectForumPlatform("https://www.2libra.com/post/tech-news/56twX_8"),
    ).toEqual({ platform: "twolibra", topicId: "56twX_8" });
    expect(detectForumPlatform("https://2libra.com/post/hot/today")).toBeNull();
    expect(
      detectForumPlatform(
        "https://2libra.com.example.com/post/health-consultation/bUSaOUc",
      ),
    ).toBeNull();
  });

  it("用节点与 shortId 生成稳定规范链接", () => {
    expect(twolibraCanonicalUrl("health-consultation", TOPIC_ID)).toBe(
      "https://2libra.com/post/health-consultation/bUSaOUc",
    );
  });
});

describe("fetchTwolibraThread", () => {
  it("读取公开主楼并逐页拉取平铺评论，保留回复上下文", async () => {
    const fetchJson = vi.fn().mockImplementation(async (url: string) => {
      if (url.endsWith(`/posts/${TOPIC_ID}`)) return postPayload();
      const page = new URL(url).searchParams.get("page");
      return commentPayload(Number(page));
    });

    const thread = await fetchTwolibraThread(TOPIC_ID, {
      fetchJson,
      retryDelaysMs: [],
      commentPageSize: 2,
    });

    expect(thread).toMatchObject({
      platform: "twolibra",
      topicId: TOPIC_ID,
      title: "【💰】记一下验光体验",
      author: "z1gui",
      node: "健康咨询",
      replyCount: 3,
      content: "主楼第一段\n\n- 建议一\n- 建议二",
      webpageUrl: "https://2libra.com/post/health-consultation/bUSaOUc",
    });
    expect(thread.replies).toHaveLength(2);
    expect(thread.replies.map((reply) => reply.floor)).toEqual([1, 2]);
    expect(thread.replies[1]?.replyTo).toEqual({
      author: "alice",
      floor: 1,
      snippet: "第一条评论",
    });
    expect(fetchJson).toHaveBeenCalledTimes(3);
  });

  it("后续评论页失败时保留已抓内容并给出缺失提示", async () => {
    const fetchJson = vi.fn().mockImplementation(async (url: string) => {
      if (url.endsWith(`/posts/${TOPIC_ID}`)) return postPayload();
      if (url.includes("page=1")) return commentPayload(1);
      throw new Error("HTTP 503");
    });

    const thread = await fetchTwolibraThread(TOPIC_ID, {
      fetchJson,
      retryDelaysMs: [],
      commentPageSize: 2,
    });

    expect(thread.replies).toHaveLength(2);
    expect(thread.warningReason).toContain("评论第 2 页抓取失败");
  });
});

describe("extractForumPost · 2Libra", () => {
  it("组装 forum 条目、平台元数据与逐楼讨论", async () => {
    const entry = await extractForumPost(
      { platform: "twolibra", topicId: TOPIC_ID },
      {
        fetchThread: async () => ({
          platform: "twolibra",
          topicId: TOPIC_ID,
          title: "测试帖",
          author: "z1gui",
          node: "健康咨询",
          createdAt: Date.parse("2026-06-24T06:18:54.029Z"),
          replyCount: 1,
          content: "主楼内容",
          replies: [
            {
              floor: 1,
              author: "alice",
              content: "讨论内容",
              createdAt: Date.parse("2026-06-24T06:24:06.183Z"),
            },
          ],
          replyRetention: "all",
          webpageUrl: twolibraCanonicalUrl("health-consultation", TOPIC_ID),
        }),
        getSummaryConfig: () => null,
      },
    );

    expect(entry.itemType).toBe("forum");
    expect(entry.sourceUri).toBe(
      "https://2libra.com/post/health-consultation/bUSaOUc",
    );
    expect(parseVideoMetaBlock(entry.content)?.platform).toBe("2Libra");
    const sections = splitForumNoteSections(entry.content);
    expect(sections.body).toContain("主楼内容");
    expect(sections.replies).toContain("讨论内容");
  });
});
