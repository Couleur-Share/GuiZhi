import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import type { KnowledgeItem } from "@guizhi/shared/types";
import { installWindowMocks } from "../../helpers/window";
import { changeLanguage, i18nReady } from "../../../src/renderer/i18n";
import { ToastProvider } from "../../../src/renderer/components/ui/Toast";
import { AiHandoffButton } from "../../../src/renderer/components/library/AiHandoffButton";
import { useCollectionStore } from "../../../src/renderer/stores/collection.store";

const TRANSCRIPT = "大家好，今天聊聊状态管理，先说结论。".repeat(20);

function makeItem(overrides?: Partial<KnowledgeItem>): KnowledgeItem {
  return {
    id: "item-1",
    title: "用 Zustand 替换 Redux 的三个前提",
    content: [
      "> 平台：哔哩哔哩 · 作者：某某 · 时长：12:52",
      "",
      "## 视频总结",
      "",
      "**一、核心结论**",
    ].join("\n"),
    summary: null,
    transcript: TRANSCRIPT,
    itemType: "video",
    status: "active",
    collectionId: "col-1",
    isFavorite: false,
    isPinned: false,
    sourceUri: "https://www.bilibili.com/video/BV1xx",
    deletedAt: null,
    createdAt: Date.now(),
    updatedAt: Date.now(),
    tags: [{ id: "t1", name: "Zustand", colorKey: "gray" }],
    ...overrides,
  };
}

let exportCalls: { title: string; text: string }[];

function setup(item = makeItem()) {
  exportCalls = [];
  installWindowMocks({
    api: {
      knowledge: { get: vi.fn(async () => item) },
      backup: {
        exportAiHandoff: vi.fn(async (request: { title: string; text: string }) => {
          exportCalls.push(request);
          return { success: true, filePath: "D:\\out.md" };
        }),
      },
    },
  });
  useCollectionStore.setState({
    collections: [
      {
        id: "col-1",
        name: "前端",
        icon: "🧩",
        sortOrder: 0,
        createdAt: 0,
        updatedAt: 0,
      },
    ],
  });
  return render(
    <ToastProvider>
      <AiHandoffButton item={item} />
    </ToastProvider>,
  );
}

/** userEvent.setup() 会用自己的剪贴板 stub 换掉全局 mock，所以直接读回来 */
function copiedText(): Promise<string> {
  return navigator.clipboard.readText();
}

beforeAll(async () => {
  await i18nReady;
  await changeLanguage("zh");
  // copyTextToClipboard 只在安全上下文里走 clipboard API，jsdom 默认不是
  Object.defineProperty(globalThis, "isSecureContext", {
    configurable: true,
    value: true,
  });
});

beforeEach(() => {
  vi.clearAllMocks();
});

describe("复制给 AI 按钮", () => {
  it("主按钮一步复制完整版：元信息进 front matter，文字稿在里面", async () => {
    const user = userEvent.setup();
    setup();

    await user.click(
      screen.getByRole("button", { name: "复制给 AI（含完整文字稿）" }),
    );

    const text = await copiedText();
    expect(text).toContain('source: "https://www.bilibili.com/video/BV1xx"');
    expect(text).toContain('platform: "哔哩哔哩"');
    expect(text).toContain('collection: "前端"');
    expect(text).toContain("## 口播文字稿");
    expect(text).toContain(TRANSCRIPT);
  });

  it("菜单里的精简版略去文字稿，但留下字数说明", async () => {
    const user = userEvent.setup();
    setup();

    await user.click(screen.getByRole("button", { name: "更多 AI 导出方式" }));
    await user.click(
      screen.getByRole("button", { name: "复制精简版（只要总结）" }),
    );

    const text = await copiedText();
    expect(text).not.toContain(TRANSCRIPT);
    expect(text).toContain("本次未包含");
    expect(text).toContain("**一、核心结论**");
  });

  it("另存为把完整版交给主进程落盘", async () => {
    const user = userEvent.setup();
    setup();

    await user.click(screen.getByRole("button", { name: "更多 AI 导出方式" }));
    await user.click(screen.getByRole("button", { name: "另存为 .md 文件…" }));

    expect(exportCalls).toHaveLength(1);
    expect(exportCalls[0].title).toBe("用 Zustand 替换 Redux 的三个前提");
    expect(exportCalls[0].text).toContain(TRANSCRIPT);
  });

  it("空条目不给按，避免导出一份只有 front matter 的壳", () => {
    setup(makeItem({ content: "", transcript: null }));

    expect(
      screen.getByRole("button", { name: "复制给 AI（含完整文字稿）" }),
    ).toBeDisabled();
  });
});
