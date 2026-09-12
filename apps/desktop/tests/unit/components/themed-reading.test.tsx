import {
  act,
  fireEvent,
  render,
  renderHook,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import type {
  ThemedReadingTask,
  ThemedReadingVersion,
} from "@guizhi/shared/types/themed-reading";
import { installWindowMocks } from "../../helpers/window";
import { changeLanguage, i18nReady } from "../../../src/renderer/i18n";
import { ToastProvider } from "../../../src/renderer/components/ui/Toast";
import { ThemedReadingSetup } from "../../../src/renderer/components/themed-reading/ThemedReadingSetup";
import { useThemedReading } from "../../../src/renderer/components/themed-reading/use-themed-reading";
import { useKnowledgeStore } from "../../../src/renderer/stores/knowledge.store";
import {
  loadContentReadingMemory,
  patchContentReadingMemory,
} from "../../../src/renderer/components/library/reading-memory";
import { ThemedReadingFrame } from "../../../src/renderer/components/themed-reading/ThemedReadingFrame";
import { ThemedReadingTasks } from "../../../src/renderer/components/themed-reading/ThemedReadingTasks";
import { ThemedReadingPane } from "../../../src/renderer/components/themed-reading/ThemedReadingPane";
import type { KnowledgeItem } from "@guizhi/shared/types";
import { ThemedReadingStatus } from "../../../src/renderer/components/themed-reading/ThemedReadingStatus";

const models = { text: "text-model", image: "image-model" };
let generate: ReturnType<typeof vi.fn>,
  cancel: ReturnType<typeof vi.fn>,
  unsubscribe: ReturnType<typeof vi.fn>;
let progress: (task: ThemedReadingTask) => void;

beforeAll(async () => {
  await i18nReady;
  await changeLanguage("zh");
});
beforeEach(() => {
  localStorage.clear();
  generate = vi.fn().mockResolvedValue({ success: true });
  cancel = vi.fn().mockResolvedValue({ success: true });
  unsubscribe = vi.fn();
  installWindowMocks({
    api: {
      themedReading: {
        get: vi.fn().mockResolvedValue({ success: true, page: null, models }),
        generate,
        cancel,
        resume: vi.fn().mockResolvedValue({ success: true }),
        onProgress: vi.fn().mockImplementation((callback) => {
          progress = callback;
          return unsubscribe;
        }),
      },
      log: { appError: vi.fn() },
    },
  });
  useKnowledgeStore.setState({
    hasUnsavedChanges: false,
    flushPendingSave: vi.fn().mockResolvedValue(undefined),
  });
});

describe("主题阅读页的生成选项", () => {
  it("首次生成默认允许最多三张图片，一次点击直接启动", async () => {
    const submit = vi.fn().mockResolvedValue(true),
      close = vi.fn(),
      user = userEvent.setup();
    render(
      <ThemedReadingSetup
        title="啤酒文章"
        sourceKind="body"
        searchConfigured={true}
        models={models}
        busy={false}
        onClose={close}
        onGenerate={submit}
      />,
    );
    expect(await screen.findByText("啤酒文章 · 正文")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "生成 AI 阅读页" }));
    expect(submit).toHaveBeenCalledWith({
      style: "",
      generateImages: true,
      maxImages: 3,
      fromCurrent: false,
      research: false,
        action: "create",
        enhancedInteraction: true,
        researchDepth: "standard",
    });
    await waitFor(() => expect(close).toHaveBeenCalledOnce());
  });
  it("未配置搜索也可默认生成，启用默认开关后仍可单次关闭", async () => {
    const submit = vi.fn().mockResolvedValue(true);
    const props = { title: "文章", sourceKind: "body" as const, models, busy: false, onClose: vi.fn(), onGenerate: submit };
    const view = render(<ThemedReadingSetup {...props} />);
    expect(screen.getByRole("checkbox", { name: "联网补充资料并查证" })).not.toBeChecked();
    expect(screen.getByRole("button", { name: "生成 AI 阅读页" })).toBeEnabled();
    view.unmount();
    render(<ThemedReadingSetup {...props} defaultResearch current={{ style: "", generateImages: false, maxImages: 0, research: false }} />);
    fireEvent.click(screen.getByRole("button", { name: "调整方式" }));
    fireEvent.click(screen.getByRole("option", { name: "调整内容与设计" }));
    expect(screen.getByRole("checkbox", { name: "联网补充资料并查证" })).toBeChecked();
    expect(screen.getByRole("button", { name: "生成 AI 阅读页" })).toBeDisabled();
    fireEvent.click(screen.getByRole("checkbox", { name: "联网补充资料并查证" }));
    fireEvent.click(screen.getByRole("button", { name: "生成 AI 阅读页" }));
    await waitFor(() => expect(submit).toHaveBeenCalledWith(expect.objectContaining({ research: false })));
  });
  it("调整已有页面默认复用素材，不自动产生新的生图请求", async () => {
    const submit = vi.fn().mockResolvedValue(true),
      user = userEvent.setup();
    render(
      <ThemedReadingSetup
        title="啤酒文章"
        sourceKind="body"
        current={{ style: "琥珀色", generateImages: true, maxImages: 5 }}
        searchConfigured={true}
        models={models}
        busy={false}
        onClose={() => {}}
        onGenerate={submit}
      />,
    );
    expect(
      screen.getByRole("checkbox", { name: "生成主题元素图片" }),
    ).not.toBeChecked();
    await user.click(screen.getByRole("button", { name: "生成 AI 阅读页" }));
    expect(submit).toHaveBeenCalledWith({
      style: "琥珀色",
      generateImages: false,
      maxImages: 3,
      fromCurrent: true,
      research: false,
        action: "redesign",
        enhancedInteraction: true,
        researchDepth: "standard",
    });
  });
  it("缺少文本模型时提供设置引导，禁止提交", () => {
    render(
      <ThemedReadingSetup
        title="啤酒文章"
        sourceKind="body"
        searchConfigured={true}
        models={{ text: null, image: null }}
        busy={false}
        onClose={() => {}}
        onGenerate={vi.fn()}
      />,
    );
    expect(
      screen.getByRole("button", { name: "生成 AI 阅读页" }),
    ).toBeDisabled();
    expect(screen.getByRole("button", { name: "配置模型" })).toBeEnabled();
  });
  it("没有生图模型时仍可仅排版，同时保留配置入口", () => {
    render(
      <ThemedReadingSetup
        title="啤酒文章"
        sourceKind="body"
        searchConfigured={true}
        models={{ text: "text-model", image: null }}
        busy={false}
        onClose={() => {}}
        onGenerate={vi.fn()}
      />,
    );
    expect(
      screen.getByRole("button", { name: "生成 AI 阅读页" }),
    ).toBeEnabled();
    expect(screen.getByRole("button", { name: "配置模型" })).toBeEnabled();
    expect(
      screen.getByText("尚未配置生图模型，仍可仅排版。"),
    ).toBeInTheDocument();
  });
});

describe("主题任务和正文边界", () => {
  it("正文保存失败时不向主进程提交生成", async () => {
    useKnowledgeStore.setState({ hasUnsavedChanges: true });
    const { result } = renderHook(() => useThemedReading("a", "body", "正文"), {
      wrapper: ToastProvider,
    });
    await waitFor(() => expect(result.current.result).not.toBeNull());
    let ok: boolean;
    await act(async () => {
      ok = await result.current.generate({
        style: "",
        generateImages: false,
        maxImages: 3,
      });
    });
    expect(ok!).toBe(false);
    expect(generate).not.toHaveBeenCalled();
    expect(screen.getByText("生成 AI 阅读页失败")).toBeInTheDocument();
  });
  it("按条目和来源过滤进度，关闭面板只退订", async () => {
    const { result, unmount } = renderHook(
      () => useThemedReading("a", "body", "正文"),
      { wrapper: ToastProvider },
    );
    await waitFor(() => expect(result.current.result).not.toBeNull());
    const task = {
      id: "job",
      itemId: "a",
      sourceKind: "summary",
      state: "running",
      stage: "images",
    } as ThemedReadingTask;
    act(() => progress(task));
    expect(result.current.task).toBeNull();
    act(() => progress({ ...task, sourceKind: "body" }));
    expect(result.current.task?.id).toBe("job");
    unmount();
    expect(unsubscribe).toHaveBeenCalled();
    expect(cancel).not.toHaveBeenCalled();
  });
  it("正文与讨论总结的主题视图记忆互不覆盖", () => {
    patchContentReadingMemory("a", {
      tab: "body",
      themedBySource: { body: true },
    });
    patchContentReadingMemory("a", {
      tab: "summary",
      themedBySource: { summary: false },
    });
    patchContentReadingMemory("a", { scrollTopByTab: { summary: 240 } });
    expect(loadContentReadingMemory("a")?.themedBySource).toEqual({
      body: true,
      summary: false,
    });
    expect(loadContentReadingMemory("a")?.scrollTopByTab.summary).toBe(240);
  });
  it("较早开始的读取不会覆盖新收到的生成进度", async () => {
    let finish: (value: unknown) => void;
    vi.mocked(window.api.themedReading.get).mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finish = resolve;
        }),
    );
    const { result } = renderHook(() => useThemedReading("a", "body", "正文"), {
      wrapper: ToastProvider,
    });
    const task = {
      id: "latest",
      itemId: "a",
      sourceKind: "body",
      state: "running",
      stage: "images",
    } as ThemedReadingTask;
    act(() => progress(task));
    await act(async () => finish({ success: true, page: null, models }));
    expect(result.current.task?.id).toBe("latest");
  });
});

describe("主题页隔离阅读", () => {
  const page = {
    id: "page",
    itemId: "a",
    sourceKind: "body",
    assets: [],
  } as ThemedReadingVersion;
  it("只信任当前 iframe 的匹配实例消息，保留独立源隔离", () => {
    const openFind = vi.fn();
    render(
      <ThemedReadingFrame
        document="<p>测试</p>"
        page={page}
        instanceId="instance"
        onFindOpen={openFind}
      />,
    );
    const iframe = screen.getByLabelText("AI 重构阅读") as HTMLIFrameElement;
    expect(iframe).toHaveAttribute("sandbox", "allow-scripts");
    expect(iframe).toHaveAttribute("referrerpolicy", "no-referrer");
    const send = (
      origin: string,
      id: string,
      source = iframe.contentWindow,
    ) => {
      act(() => {
        window.dispatchEvent(
          new MessageEvent("message", {
            origin,
            source,
            data: { id, type: "find-open", value: null },
          }),
        );
      });
    };
    send("https://example.com", "instance");
    send("null", "wrong");
    send("null", "instance", window);
    expect(openFind).not.toHaveBeenCalled();
    send("null", "instance");
    expect(openFind).toHaveBeenCalledOnce();
  });
  it("拒绝非网页外链和带凭据的链接", () => {
    const open = vi.spyOn(window, "open").mockImplementation(() => null);
    render(
      <ThemedReadingFrame
        document="<p>测试</p>"
        page={page}
        instanceId="instance"
      />,
    );
    const iframe = screen.getByLabelText("AI 重构阅读") as HTMLIFrameElement;
    for (const value of [
      "javascript:alert(1)",
      "file:///private",
      "https://user:secret@example.com",
      "https://example.com/article",
    ]) {
      act(() => {
        window.dispatchEvent(
          new MessageEvent("message", {
            origin: "null",
            source: iframe.contentWindow,
            data: { id: "instance", type: "link", value },
          }),
        );
      });
    }
    expect(open).toHaveBeenCalledTimes(1);
    expect(open).toHaveBeenCalledWith(
      "https://example.com/article",
      "_blank",
      "noopener,noreferrer",
    );
    open.mockRestore();
  });
  it("忽略过期查找响应，仅采用当前请求的计数", () => {
    const count = vi.fn();
    render(
      <ThemedReadingFrame
        document="<p>测试</p>"
        page={page}
        instanceId="instance"
        findQuery="啤酒"
        onFindCount={count}
      />,
    );
    const iframe = screen.getByLabelText("AI 重构阅读") as HTMLIFrameElement;
    const post = vi.spyOn(iframe.contentWindow!, "postMessage");
    fireEvent.load(iframe);
    expect(
      post.mock.calls.find(([value]) => value.type === "appearance")?.[0].value,
    ).toMatchObject({
      theme: document.documentElement.classList.contains("dark")
        ? "dark"
        : "light",
      fontFamily: getComputedStyle(document.body).fontFamily,
    });
    const latest = post.mock.calls.find(([value]) => value.type === "find")![0]
      .value.request;
    for (const request of [latest - 1, latest])
      act(() => {
        window.dispatchEvent(
          new MessageEvent("message", {
            origin: "null",
            source: iframe.contentWindow,
            data: {
              id: "instance",
              type: "find-result",
              value: { request, count: 3 },
            },
          }),
        );
      });
    expect(count).toHaveBeenCalledTimes(1);
    expect(count).toHaveBeenCalledWith(3);
  });
  it("处理中心读取失败显示错误和重试，不伪装正常空态", async () => {
    installWindowMocks({
      api: {
        themedReading: {
          listTasks: vi
            .fn()
            .mockResolvedValue({ success: false, error: "数据库无法读取" }),
          onProgress: vi.fn().mockReturnValue(() => {}),
        },
      },
    });
    render(
      <ToastProvider>
        <ThemedReadingTasks onBack={() => {}} />
      </ToastProvider>,
    );
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "数据库无法读取",
    );
    expect(screen.queryByText(/还没有主题页任务/)).not.toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "重试", exact: true }),
    ).toBeEnabled();
  });
  it("回收站允许查看和导出，禁用所有改写入口", async () => {
    vi.mocked(window.api.themedReading.get).mockResolvedValue({
      success: true,
      page: {
        ...page,
        warnings: [],
        options: { style: "", generateImages: false, maxImages: 3 },
      },
      previous: true,
      models,
      document: "<p>保存的主题页</p>",
    });
    render(
      <ToastProvider>
        <ThemedReadingPane
          item={
            { id: "a", content: "正文", deletedAt: Date.now() } as KnowledgeItem
          }
          sourceKind="body"
          findQuery=""
          findIndex={0}
          onFindCount={() => {}}
          onFindOpen={() => {}}
        />
      </ToastProvider>,
    );
    fireEvent.click(
      await screen.findByRole("button", { name: "阅读页设置", exact: true }),
    );
    expect(
      screen.getByRole("menuitem", { name: "重新设计阅读页", exact: true }),
    ).toBeDisabled();
    expect(
      screen.getByRole("menuitem", { name: "按最新原文重新生成", exact: true }),
    ).toBeDisabled();
    expect(
      screen.getByRole("menuitem", {
        name: "恢复上一版 AI 阅读页",
        exact: true,
      }),
    ).toBeDisabled();
    expect(
      screen.getByRole("menuitem", { name: "删除 AI 阅读页", exact: true }),
    ).toBeDisabled();
    expect(
      screen.getByRole("menuitem", { name: "导出 HTML", exact: true }),
    ).toBeEnabled();
  });
  it("首次进入无页面时自动显示带来源的设置，关闭后不重复弹出", async () => {
    const user = userEvent.setup();
    const item = {
      id: "a",
      title: "啤酒讨论",
      content: "总结正文",
      itemType: "forum",
    } as KnowledgeItem;
    const pane = (content: string) => (
      <ToastProvider>
        <ThemedReadingPane
          item={{ ...item, content }}
          sourceKind="summary"
          findQuery=""
          findIndex={0}
          onFindCount={() => {}}
          onFindOpen={() => {}}
        />
      </ToastProvider>
    );
    const view = render(pane(item.content));
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByText("啤酒讨论 · 讨论总结")).toBeInTheDocument();
    await user.click(
      within(dialog).getByRole("button", { name: "取消", exact: true }),
    );
    await waitFor(() =>
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
    );
    view.rerender(pane("总结正文更新"));
    await waitFor(() =>
      expect(window.api.themedReading.get).toHaveBeenCalledTimes(2),
    );
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    await user.click(
      screen.getByRole("button", { name: "生成阅读页", exact: true }),
    );
    expect(await screen.findByRole("dialog")).toBeInTheDocument();
  });
  it("生成计划显示实际新增张数，独立于含原图的进度总数", () => {
    const task = {
      state: "running",
      stage: "images",
      completed: 1,
      total: 5,
      plannedImages: 3,
    } as ThemedReadingTask;
    render(
      <ThemedReadingStatus
        task={task}
        onCancel={() => {}}
        onResume={() => {}}
      />,
    );
    expect(screen.getByText("计划新增 3 张主题图片")).toBeInTheDocument();
    expect(screen.getByText(/生成主题图片 · 1\/5/)).toBeInTheDocument();
  });
  it.each(["queued", "running", "failed", "cancelled", "interrupted"] as const)(
    "首次生成的 %s 状态不再显示开始设计空态",
    async (state) => {
      const task = {
        id: "job",
        itemId: "a",
        sourceKind: "body",
        state,
        stage: "images",
        completed: 0,
        total: 1,
      } as ThemedReadingTask;
      vi.mocked(window.api.themedReading.get).mockResolvedValue({
        success: true,
        page: null,
        models,
        task,
      });
      render(
        <ToastProvider>
          <ThemedReadingPane
            item={{ id: "a", content: "正文" } as KnowledgeItem}
            sourceKind="body"
            findQuery=""
            findIndex={0}
            onFindCount={() => {}}
            onFindOpen={() => {}}
          />
        </ToastProvider>,
      );
      const active = state === "running" || state === "queued";
      expect(
        await screen.findByRole("button", {
          name: active ? "停止生成" : "继续生成",
          exact: true,
        }),
      ).toBeEnabled();
      expect(
        screen.queryByRole("button", { name: "开始设计", exact: true }),
      ).not.toBeInTheDocument();
      expect(
        screen.queryByText("为这篇内容设计一个阅读页"),
      ).not.toBeInTheDocument();
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
      const details = screen.getByText("查看生成详情").closest("details");
      expect(details).not.toHaveAttribute("open");
      fireEvent.click(
        screen.getByRole("button", {
          name: active ? "停止生成" : "继续生成",
          exact: true,
        }),
      );
      await waitFor(() =>
        expect(
          active ? cancel : window.api.themedReading.resume,
        ).toHaveBeenCalledWith("job"),
      );
    },
  );
  it("区分模型请求和已保存新图，旧任务缺失用量不显示零", () => {
    const task = {
      state: "completed",
      stage: "done",
      completed: 3,
      total: 3,
    } as ThemedReadingTask;
    const view = render(
      <ThemedReadingStatus
        task={task}
        onCancel={() => {}}
        onResume={() => {}}
      />,
    );
    expect(screen.getByText("此任务未记录用量")).toBeInTheDocument();
    expect(screen.queryByText("文本请求 0 次")).not.toBeInTheDocument();
    view.rerender(
      <ThemedReadingStatus
        task={{
          ...task,
          usage: { textCalls: 2, imageCalls: 3, imagesSaved: 1 },
        }}
        onCancel={() => {}}
        onResume={() => {}}
      />,
    );
    expect(screen.getByText("文本请求 2 次")).toBeInTheDocument();
    expect(screen.getByText("生图请求 3 次")).toBeInTheDocument();
    expect(screen.getByText("新图保存 1 张")).toBeInTheDocument();
    expect(screen.queryByText("此任务未记录用量")).not.toBeInTheDocument();
  });
  it("完成事件到页面载入之间不闪回开始设计", async () => {
    const task = {
      id: "job",
      itemId: "a",
      sourceKind: "body",
      state: "running",
      stage: "images",
      completed: 0,
      total: 1,
    } as ThemedReadingTask;
    vi.mocked(window.api.themedReading.get).mockResolvedValueOnce({
      success: true,
      page: null,
      models,
      task,
    });
    render(
      <ToastProvider>
        <ThemedReadingPane
          item={{ id: "a", content: "正文" } as KnowledgeItem}
          sourceKind="body"
          findQuery=""
          findIndex={0}
          onFindCount={() => {}}
          onFindOpen={() => {}}
        />
      </ToastProvider>,
    );
    await screen.findByRole("button", { name: "停止生成", exact: true });
    let finish: (value: unknown) => void;
    vi.mocked(window.api.themedReading.get).mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finish = resolve;
        }),
    );
    act(() => progress({ ...task, state: "completed", stage: "done" }));
    expect(screen.getByRole("status")).toHaveTextContent(
      "正在载入完成的阅读页…",
    );
    expect(
      screen.queryByRole("button", { name: "开始设计", exact: true }),
    ).not.toBeInTheDocument();
    await act(async () =>
      finish({
        success: true,
        models,
        page: { ...page, warnings: [], options: {} },
        document: "<p>完成的阅读页</p>",
      }),
    );
    expect(screen.getByLabelText("AI 重构阅读")).toBeInTheDocument();
  });
});

it("设计状态展示真实输出进度与修复状态，离开设计后隐藏", () => {
  const task = {id:"progress",itemId:"item",sourceKind:"body",versionId:"version",title:"测试",state:"running",stage:"design",completed:0,total:0,createdAt:1,updatedAt:1,designProgress:{attempt:1,receivedChars:1234}} as ThemedReadingTask;
  const props = {onCancel:vi.fn(),onResume:vi.fn()};
  const view = render(<ThemedReadingStatus task={task} {...props} />);
  expect(screen.getByText("正在输出页面 · 已接收 1234 字符")).toBeInTheDocument();
  view.rerender(<ThemedReadingStatus task={{...task,designProgress:{attempt:2,receivedChars:0}}} {...props} />);
  expect(screen.getByText("调整页面设计")).toBeInTheDocument();
  expect(screen.getByText("正在构思页面布局")).toBeInTheDocument();
  view.rerender(<ThemedReadingStatus task={{...task,stage:"assemble"}} {...props} />);
  expect(screen.queryByText("正在构思页面布局")).not.toBeInTheDocument();
});
