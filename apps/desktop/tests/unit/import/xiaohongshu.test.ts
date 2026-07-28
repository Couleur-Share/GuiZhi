import { describe, expect, it, vi } from "vitest";
import fs from "fs";
import os from "os";
import path from "path";

// xiaohongshu → safe-fetch → network-proxy 引用 electron，单测中替换为空实现
vi.mock("electron", () => ({
  session: { defaultSession: {} },
  app: {},
}));

import {
  cleanNoteText,
  extractXhsNoteId,
  fetchXiaohongshuNote,
  parseXiaohongshuNote,
  sliceInitialStateJson,
  sniffImageExtension,
  xiaohongshuNoteUrl,
} from "../../../src/main/services/import/xiaohongshu";

const NOTE_ID = "6a59e7f3000000000301fc49";
const SHARE_URL = `https://www.xiaohongshu.com/discovery/item/${NOTE_ID}?xsec_token=CBai8aBe1zi_hNlq7nojlsTLX0Z9M9ZhGy22tGMBTxhUI=&xsec_source=pc_share`;

/**
 * 真实页面的形态：`__INITIAL_STATE__` 之外还有别的脚本，且对象里散着
 * 几十处裸 `undefined`（小红书 SSR 直接序列化 JS 对象的结果）。
 */
function stateHtml(note: unknown, noteId: string = NOTE_ID): string {
  const payload = JSON.stringify({
    global: { serverTime: 1785205919489 },
    user: { loggedIn: false, userFetchingStatus: "__UNDEF__" },
    note: {
      firstNoteId: noteId,
      noteDetailMap: note
        ? { [noteId]: { note, currentTime: 1785205919522 } }
        : {},
      serverRequestInfo: { state: "success", errorCode: 0 },
    },
  }).replace(/"__UNDEF__"/g, "undefined");
  return `<!doctype html><html><head><title>小红书</title></head><body><script>window.__INITIAL_STATE__=${payload}</script><script>window.other={"a":1}</script></body></html>`;
}

function imageEntry(index: number) {
  const base = `http://sns-webpic-qc.xhscdn.com/202607281031/hash${index}/spectrum/file${index}`;
  return {
    urlDefault: `${base}!nd_dft_wlteh_jpg_3`,
    urlPre: `${base}!nd_prv_wlteh_jpg_3`,
    infoList: [
      { imageScene: "WB_PRV", url: `${base}!nd_prv_wlteh_jpg_3` },
      { imageScene: "WB_DFT", url: `${base}!nd_dft_wlteh_jpg_3` },
    ],
    width: 1242,
    height: 1656,
  };
}

const IMAGE_NOTE = {
  noteId: NOTE_ID,
  type: "normal",
  title: "AI漫剧培训实战课程丨12天独立出片",
  desc: "司晨视觉AI漫剧实战班。\n1. 先跑通流程。\n#AI漫剧[话题]# #AIGC[话题]#",
  user: { userId: "6706", nickname: "司晨视觉" },
  imageList: [imageEntry(0), imageEntry(1)],
  tagList: [{ id: "1", name: "AI漫剧", type: "topic" }],
};

const VIDEO_NOTE = {
  noteId: NOTE_ID,
  type: "video",
  title: "以AI为笔，赴创作之途",
  desc: "学员阶段性创作混剪",
  user: { userId: "6706", nickname: "司晨视觉" },
  // 视频笔记同样带 imageList，装的是封面
  imageList: [imageEntry(0)],
  video: {
    media: {
      stream: {
        h264: [
          {
            masterUrl: "http://sns-video-bd.xhscdn.com/master.mp4",
            backupUrls: ["http://sns-video-hw.xhscdn.com/backup.mp4"],
            duration: 128073,
          },
        ],
        h265: [],
      },
    },
  },
};

describe("extractXhsNoteId / xiaohongshuNoteUrl", () => {
  it("认得两种站内路径，大小写归一", () => {
    expect(extractXhsNoteId(SHARE_URL)).toBe(NOTE_ID);
    expect(
      extractXhsNoteId(`https://www.xiaohongshu.com/explore/${NOTE_ID}`),
    ).toBe(NOTE_ID);
    expect(
      extractXhsNoteId(`https://www.xiaohongshu.com/explore/${NOTE_ID.toUpperCase()}`),
    ).toBe(NOTE_ID);
  });

  it("短链与非笔记页取不到 ID", () => {
    expect(extractXhsNoteId("http://xhslink.com/a/abc123")).toBeNull();
    expect(
      extractXhsNoteId("https://www.xiaohongshu.com/user/profile/6706"),
    ).toBeNull();
    expect(extractXhsNoteId("not-a-url")).toBeNull();
  });

  it("规范链接不带 token：分享链每次都不同，去重只能认这一个", () => {
    expect(xiaohongshuNoteUrl(NOTE_ID)).toBe(
      `https://www.xiaohongshu.com/explore/${NOTE_ID}`,
    );
  });
});

describe("sliceInitialStateJson", () => {
  it("串外的 undefined 补成 null，整段能被 JSON.parse", () => {
    const json = sliceInitialStateJson(stateHtml(IMAGE_NOTE));
    expect(json).not.toBeNull();
    expect(() => JSON.parse(json as string)).not.toThrow();
  });

  it("字符串里的 undefined 一个字都不能动", () => {
    // 这是知识库应用，正文里出现这个词再正常不过；无脑全局替换会改坏文案
    const html = stateHtml({
      ...IMAGE_NOTE,
      desc: "报错是 Cannot read properties of undefined，注意 undefined 与 null 的区别",
    });
    const state = JSON.parse(sliceInitialStateJson(html) as string);
    expect(state.note.noteDetailMap[NOTE_ID].note.desc).toBe(
      "报错是 Cannot read properties of undefined，注意 undefined 与 null 的区别",
    );
    // 串外的那个照常被补上
    expect(state.user.userFetchingStatus).toBeNull();
  });

  it("赋值语句后面还有脚本时按花括号配平切，不多吃", () => {
    const json = sliceInitialStateJson(stateHtml(IMAGE_NOTE)) as string;
    expect(json.endsWith("}")).toBe(true);
    expect(json).not.toContain("window.other");
  });

  it("页面里没有这个变量时返回 null", () => {
    expect(sliceInitialStateJson("<html><body>登录后查看</body></html>")).toBeNull();
  });
});

describe("parseXiaohongshuNote（图文）", () => {
  it("标题 / 作者 / 文案照实取出，话题标记清干净", () => {
    const note = parseXiaohongshuNote(stateHtml(IMAGE_NOTE), NOTE_ID);
    expect(note.kind).toBe("note");
    expect(note.title).toBe("AI漫剧培训实战课程丨12天独立出片");
    expect(note.authoredTitle).toBe(true);
    expect(note.author).toBe("司晨视觉");
    expect(note.description).toContain("司晨视觉AI漫剧实战班。");
    expect(note.description).toContain("#AI漫剧 #AIGC");
    expect(note.description).not.toContain("[话题]#");
    expect(note.webpageUrl).toBe(
      `https://www.xiaohongshu.com/explore/${NOTE_ID}`,
    );
  });

  it("配图逐张给出镜像：升到 https 并按清晰度去重", () => {
    const note = parseXiaohongshuNote(stateHtml(IMAGE_NOTE), NOTE_ID);
    expect(note.imageMirrors).toHaveLength(2);
    // urlDefault 与 infoList 的 WB_DFT 是同一个地址，去重后只剩清晰版 + 预览版
    expect(note.imageMirrors[0]).toEqual([
      "https://sns-webpic-qc.xhscdn.com/202607281031/hash0/spectrum/file0!nd_dft_wlteh_jpg_3",
      "https://sns-webpic-qc.xhscdn.com/202607281031/hash0/spectrum/file0!nd_prv_wlteh_jpg_3",
    ]);
    expect(note.playUrls).toEqual([]);
    expect(note.durationSeconds).toBeNull();
  });

  it("没有标题字段时退回文案首行，并标明不是作者写的", () => {
    const note = parseXiaohongshuNote(
      stateHtml({ ...IMAGE_NOTE, title: "" }),
      NOTE_ID,
    );
    expect(note.title).toBe("司晨视觉AI漫剧实战班。");
    expect(note.authoredTitle).toBe(false);
    // 标题就是文案首行时不重复写进简介
    expect(note.description).toContain("1. 先跑通流程。");
  });

  it("URL 里取不到 ID 时按页面自报的 firstNoteId 走", () => {
    const note = parseXiaohongshuNote(stateHtml(IMAGE_NOTE), null);
    expect(note.noteId).toBe(NOTE_ID);
  });
});

describe("parseXiaohongshuNote（视频）", () => {
  it("主源与备源都收进来，时长毫秒换算成秒", () => {
    const note = parseXiaohongshuNote(stateHtml(VIDEO_NOTE), NOTE_ID);
    expect(note.kind).toBe("video");
    expect(note.durationSeconds).toBe(128);
    expect(note.playUrls).toEqual([
      "https://sns-video-bd.xhscdn.com/master.mp4",
      "https://sns-video-hw.xhscdn.com/backup.mp4",
    ]);
  });

  it("视频笔记的 imageList 是封面，不当配图入库", () => {
    const note = parseXiaohongshuNote(stateHtml(VIDEO_NOTE), NOTE_ID);
    expect(note.imageMirrors).toEqual([]);
  });

  it("流里没给时长时退到 capa", () => {
    const note = parseXiaohongshuNote(
      stateHtml({
        ...VIDEO_NOTE,
        video: {
          media: { stream: { h264: [{ masterUrl: "https://x/a.mp4" }] } },
          capa: { duration: 96 },
        },
      }),
      NOTE_ID,
    );
    expect(note.durationSeconds).toBe(96);
  });
});

describe("parseXiaohongshuNote（取不到笔记）", () => {
  it("noteDetailMap 为空 → 报错点名 xsec_token", () => {
    expect(() => parseXiaohongshuNote(stateHtml(null), NOTE_ID)).toThrow(
      /xsec_token/,
    );
  });

  it("页面里根本没有状态对象 → 报改版而不是报缺令牌", () => {
    expect(() => parseXiaohongshuNote("<html></html>", NOTE_ID)).toThrow(
      /改版/,
    );
  });
});

describe("fetchXiaohongshuNote", () => {
  it("一次请求拿到笔记；短链由重定向落到带 token 的站内地址", async () => {
    const requested: string[] = [];
    const note = await fetchXiaohongshuNote("http://xhslink.com/a/abc", undefined, {
      fetchPage: async (url) => {
        requested.push(url);
        return { html: stateHtml(IMAGE_NOTE), finalUrl: SHARE_URL };
      },
    });
    expect(requested).toEqual(["http://xhslink.com/a/abc"]);
    expect(note.noteId).toBe(NOTE_ID);
  });

  it("被打到站内 404 → 说清是令牌问题，而不是含糊的没有内容", async () => {
    await expect(
      fetchXiaohongshuNote(
        `https://www.xiaohongshu.com/explore/${NOTE_ID}`,
        undefined,
        {
          fetchPage: async () => ({
            // 404 页同样带 __INITIAL_STATE__，不先认出来就会报错到解析那一层
            html: stateHtml(null),
            finalUrl:
              "https://www.xiaohongshu.com/404?source=/404/sec_KWRGZAkV&redirectPath=x",
          }),
        },
      ),
    ).rejects.toThrow(/xsec_token/);
  });
});

describe("cleanNoteText", () => {
  it("话题还原成界面上的样子，普通井号不动", () => {
    expect(cleanNoteText("#AI漫剧[话题]# 收工")).toBe("#AI漫剧 收工");
    expect(cleanNoteText("#RAG #AI编程")).toBe("#RAG #AI编程");
    expect(cleanNoteText("issue #12 已修")).toBe("issue #12 已修");
  });
});

describe("sniffImageExtension", () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "guizhi-sniff-"));

  function write(name: string, bytes: number[]): string {
    const filePath = path.join(dir, name);
    fs.writeFileSync(filePath, Buffer.from(bytes));
    return filePath;
  }

  it("按文件头判格式，不看 URL", async () => {
    // 小红书的图片地址结尾是 `!nd_dft_wlteh_jpg_3`，压根没有扩展名，
    // 而 OCR 的 mime 是按扩展名推的，猜错会给视觉模型送一张标错格式的图
    expect(await sniffImageExtension(write("a", [0xff, 0xd8, 0xff, 0xe0]))).toBe(
      ".jpg",
    );
    expect(
      await sniffImageExtension(
        write("b", [0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]),
      ),
    ).toBe(".png");
    expect(
      await sniffImageExtension(write("c", [0x47, 0x49, 0x46, 0x38, 0x39, 0x61])),
    ).toBe(".gif");
    expect(
      await sniffImageExtension(
        write("d", [
          0x52, 0x49, 0x46, 0x46, 0x00, 0x00, 0x00, 0x00, 0x57, 0x45, 0x42,
          0x50,
        ]),
      ),
    ).toBe(".webp");
  });

  it("认不出来按 JPEG 存：实测小红书的图全是 JPEG，扩展名必须给一个", async () => {
    expect(await sniffImageExtension(write("e", [0x00, 0x01, 0x02]))).toBe(
      ".jpg",
    );
  });
});
