import { act, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { InboxWorkspace } from "../../../src/renderer/components/inbox/InboxWorkspace";
import { ToastProvider } from "../../../src/renderer/components/ui/Toast";
import { useCollectionStore } from "../../../src/renderer/stores/collection.store";
import { useInboxStore } from "../../../src/renderer/stores/inbox.store";
import { useKnowledgeStore } from "../../../src/renderer/stores/knowledge.store";
import { useSemanticStore } from "../../../src/renderer/stores/semantic.store";
import { useWikiStore } from "../../../src/renderer/stores/wiki.store";

function deferred() {
  let resolve!: () => void;
  const promise = new Promise<void>((done) => {
    resolve = done;
  });
  return { promise, resolve };
}

function actionButton(title: string) {
  const card = screen.getByText(title).parentElement?.parentElement;
  if (!card) throw new Error(`找不到聚合卡：${title}`);
  return within(card).getByRole("button");
}

beforeEach(() => {
  const refresh = vi.fn(async () => undefined);
  useInboxStore.setState({
    items: [
      {
        kind: "wiki-pending",
        id: "aggregate:wiki",
        count: 2,
        createdAt: 1,
      },
      {
        kind: "semantic-pending",
        id: "aggregate:semantic",
        count: 1,
        createdAt: 1,
      },
    ],
    counts: {
      "review-required": 0,
      unclassified: 0,
      "import-issue": 0,
      "discovery-candidate": 0,
      "semantic-pending": 1,
      "wiki-pending": 1,
    },
    total: 2,
    filter: "all",
    selectionIds: [],
    isLoading: false,
    loadError: null,
    refresh,
  });
  useCollectionStore.setState({
    collections: [],
    fetchCollections: vi.fn(async () => undefined),
  });
  useSemanticStore.setState({
    isIndexing: false,
    indexedThisRun: 0,
    notice: null,
  });
  useWikiStore.setState({
    isCompiling: false,
    compileNotice: null,
  });
});

function renderWorkspace() {
  return render(
    <ToastProvider>
      <InboxWorkspace />
    </ToastProvider>,
  );
}

describe("处理中心聚合任务", () => {
  it("对已选未分类条目执行 AI 智能归类，并回执新建知识库", async () => {
    const smartClassify = vi.fn(async (_ids, _names, options) => {
      options?.onProgress?.(1, 1);
      return {
        classified: 1,
        skipped: 0,
        createdCollectionNames: ["健康养护"],
      };
    });
    useInboxStore.setState({
      items: [
        {
          kind: "unclassified",
          id: "unclassified:item-1",
          itemId: "item-1",
          title: "健康饮食记录",
          createdAt: 1,
        },
      ],
      counts: {
        "review-required": 0,
        unclassified: 1,
        "import-issue": 0,
        "discovery-candidate": 0,
        "semantic-pending": 0,
        "wiki-pending": 0,
      },
      total: 1,
      selectionIds: ["item-1"],
      smartClassify,
    });
    useCollectionStore.setState({
      collections: [
        {
          id: "collection-1",
          name: "编程开发",
          sortOrder: 0,
          createdAt: 1,
          updatedAt: 1,
        },
      ],
    });
    useKnowledgeStore.setState({ refreshAll: vi.fn(async () => undefined) });
    const user = userEvent.setup();
    renderWorkspace();

    await user.click(screen.getByRole("button", { name: "AI 智能归类 1" }));

    expect(smartClassify).toHaveBeenCalledWith(
      ["item-1"],
      ["编程开发"],
      expect.objectContaining({ signal: expect.any(AbortSignal) }),
    );
    expect(
      await screen.findByText("已智能归类 1 条，新建 1 个知识库"),
    ).toBeTruthy();
  });

  it("已完成的导入警告可标记为不再提醒", async () => {
    const acknowledgeImportWarning = vi.fn(async () => 1);
    useInboxStore.setState({
      items: [
        {
          kind: "import-issue",
          id: "import:task-warning",
          taskId: "task-warning",
          title: "有缺失的导入",
          status: "completed",
          message: "1 张附件图下载失败，已保留外链",
          resultItemId: "item-1",
          duplicateItemId: null,
          createdAt: 1,
        },
      ],
      counts: {
        "review-required": 0,
        unclassified: 0,
        "import-issue": 1,
        "discovery-candidate": 0,
        "semantic-pending": 0,
        "wiki-pending": 0,
      },
      total: 1,
      acknowledgeImportWarning,
    });
    const user = userEvent.setup();
    renderWorkspace();

    await user.click(screen.getByRole("button", { name: "不再提醒" }));

    expect(acknowledgeImportWarning).toHaveBeenCalledWith("task-warning");
    expect(
      await screen.findByText("已保留导入记录并从处理中心移除"),
    ).toBeTruthy();
  });

  it("普通条目使用主题化复选框并保持选择行为", async () => {
    useInboxStore.setState({
      items: [
        {
          kind: "unclassified",
          id: "unclassified:item-1",
          itemId: "item-1",
          title: "待整理条目",
          createdAt: 1,
        },
      ],
      counts: {
        "review-required": 0,
        unclassified: 1,
        "import-issue": 0,
        "discovery-candidate": 0,
        "semantic-pending": 0,
        "wiki-pending": 0,
      },
      total: 1,
    });
    const user = userEvent.setup();
    renderWorkspace();

    const checkbox = screen.getByRole("checkbox", { name: "选择 待整理条目" });
    expect(checkbox).toHaveClass("sr-only");
    await user.click(checkbox);
    expect(useInboxStore.getState().selectionIds).toEqual(["item-1"]);
  });

  it("全选只覆盖当前列表里的可选条目，并显示半选状态", async () => {
    useInboxStore.setState({
      items: [
        {
          kind: "unclassified",
          id: "unclassified:item-1",
          itemId: "item-1",
          title: "条目一",
          createdAt: 1,
        },
        {
          kind: "review-required",
          id: "review:item-2",
          itemId: "item-2",
          title: "条目二",
          reasons: [],
          createdAt: 1,
        },
        {
          kind: "discovery-candidate",
          id: "candidate:view-1:external-1",
          viewId: "view-1",
          externalId: "external-1",
          title: "不可批量处理的候选",
          createdAt: 1,
        },
      ],
      total: 3,
    });
    const user = userEvent.setup();
    renderWorkspace();

    const selectAll = screen.getByRole("checkbox", { name: "全选当前列表" });
    await user.click(selectAll);
    expect(useInboxStore.getState().selectionIds).toEqual(["item-1", "item-2"]);

    await user.click(screen.getByRole("checkbox", { name: "选择 条目一" }));
    expect(selectAll).toHaveAttribute("aria-checked", "mixed");
    expect((selectAll as HTMLInputElement).indeterminate).toBe(true);

    await user.click(selectAll);
    expect(useInboxStore.getState().selectionIds).toEqual(["item-1", "item-2"]);
    await user.click(selectAll);
    expect(useInboxStore.getState().selectionIds).toEqual([]);
  });

  it("Wiki 编译点击后立即显示运行态，结束后给出回执", async () => {
    const gate = deferred();
    useWikiStore.setState({
      compileNow: vi.fn(async () => {
        useWikiStore.setState({ isCompiling: true });
        await gate.promise;
        useWikiStore.setState({
          isCompiling: false,
          compileNotice: { kind: "done", message: "2/2" },
        });
      }),
    });
    const user = userEvent.setup();
    renderWorkspace();

    await user.click(actionButton("Wiki 待编译"));
    expect(actionButton("Wiki 待编译")).toHaveTextContent("编译中…");
    expect(actionButton("Wiki 待编译")).toBeDisabled();

    await act(async () => gate.resolve());
    await waitFor(() =>
      expect(screen.getByText("Wiki 编译完成（2/2）")).toBeTruthy(),
    );
    expect(actionButton("Wiki 待编译")).toHaveTextContent("立即执行");
  });

  it("语义索引点击后立即显示运行态，失败原因不会静默", async () => {
    const gate = deferred();
    useSemanticStore.setState({
      runIndexing: vi.fn(async () => {
        useSemanticStore.setState({ isIndexing: true });
        await gate.promise;
        const notice = {
          kind: "failed" as const,
          indexed: 0,
          failed: 1,
          message: "HTTP 401",
        };
        useSemanticStore.setState({ isIndexing: false, notice });
        return notice;
      }),
    });
    const user = userEvent.setup();
    renderWorkspace();

    await user.click(actionButton("语义索引待更新"));
    expect(actionButton("语义索引待更新")).toHaveTextContent("索引中…");
    expect(actionButton("语义索引待更新")).toBeDisabled();

    await act(async () => gate.resolve());
    await waitFor(() =>
      expect(screen.getByText("索引失败：HTTP 401")).toBeTruthy(),
    );
    expect(actionButton("语义索引待更新")).toHaveTextContent("立即执行");
  });
});
