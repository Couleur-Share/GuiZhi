import React, { Profiler, Suspense } from "react";
import { render } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import type { KnowledgeItem } from "@guizhi/shared/types";
import { ContentPanel } from "../../../src/renderer/components/library/ContentPanel";
import { saveContentReadingMemory } from "../../../src/renderer/components/library/reading-memory";

vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: (_key: string, fallback: string) => fallback }),
}));
vi.mock("../../../src/renderer/components/ui/Toast", () => ({
  useToast: () => ({ showToast: vi.fn() }),
}));
vi.mock("../../../src/renderer/stores/settings.store", () => ({
  useSettingsStore: (
    selector: (state: { editorMarkdownPreview: boolean }) => unknown,
  ) => selector({ editorMarkdownPreview: true }),
}));
vi.mock("../../../src/renderer/components/library/use-media-actions", () => ({
  useTranscriptActions: () => ({ transcript: "", canTranscribe: false }),
  useMediaSummaryAction: () => ({}),
  useForumDiscussionRefreshAction: () => ({}),
}));
vi.mock("../../../src/renderer/components/library/MarkdownPreview", () => ({
  MarkdownPreview: React.forwardRef<HTMLDivElement, { content: string }>(
    ({ content }, ref) => <div ref={ref}>{content}</div>,
  ),
}));

const video = {
  id: "video",
  itemType: "video",
  content: "视频正文",
} as KnowledgeItem;
const forum = {
  id: "forum",
  itemType: "forum",
  content: "## 讨论总结\n\n总结内容\n\n## 正文\n\n正文内容",
} as KnowledgeItem;
beforeEach(() => localStorage.clear());

// 检查每次 DOM 提交，而非只断言 effect 跑完后的最终标签，防止漏掉闪烁。
it.each([false, true])(
  "首次提交直接显示论坛总结（重新挂载：%s）",
  (remount) => {
    const container = document.createElement("div");
    document.body.appendChild(container);
    const tabs: string[] = [];
    const view = (item: KnowledgeItem) => (
      <Suspense>
        <Profiler
          id="panel"
          onRender={() => {
            tabs.push(
              container.querySelector('[aria-pressed="true"]')?.textContent ??
                "",
            );
          }}
        >
          <ContentPanel
            key={remount ? item.id : "panel"}
            item={item}
            isTrashed={false}
          />
        </Profiler>
      </Suspense>
    );
    const result = render(view(video), { container });
    tabs.length = 0;
    result.rerender(view(forum));
    expect(tabs.length).toBeGreaterThan(0);
    expect(tabs.every((tab) => tab === "讨论总结")).toBe(true);
  },
);

it("恢复用户选择的正文，不强制返回总结", () => {
  saveContentReadingMemory(forum.id, { tab: "body", scrollTopByTab: {} });
  const result = render(
    <Suspense>
      <ContentPanel item={forum} isTrashed={false} />
    </Suspense>,
  );
  expect(
    result.container.querySelector('[aria-pressed="true"]')?.textContent,
  ).toBe("正文");
});
