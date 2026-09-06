import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import { researchFixture } from "../../helpers/research";
import { installWindowMocks } from "../../helpers/window";
const { showToast } = vi.hoisted(() => ({ showToast: vi.fn() }));
vi.mock("../../../src/renderer/components/ui/Toast", () => ({ useToast: () => ({ showToast }) }));
vi.mock("../../../src/renderer/components/library/MarkdownPreview", () => ({ MarkdownBody: () => null }));
import { ResearchWorkspace } from "../../../src/renderer/components/research/ResearchWorkspace";
import { useResearchStore } from "../../../src/renderer/stores/research.store";
import { changeLanguage, i18nReady } from "../../../src/renderer/i18n";
import { ResearchProgress } from "../../../src/renderer/components/research/ResearchProgress";

beforeAll(async () => { await i18nReady; await changeLanguage("zh"); });

beforeEach(() => {
  installWindowMocks();
  useResearchStore.setState({ selectedRunId: "run-1", detail: researchFixture(), busy: false, error: null });
});
describe("研究任务反馈与平台分类", () => {
  it("新研究评论默认关闭，主动开启才传入；仅选 B 站时隐藏并关闭", async () => {
    installWindowMocks({ api: { collection: { list: vi.fn().mockResolvedValue([]) }, platformCapture: { getStatuses: vi.fn().mockResolvedValue([]) } } });
    const create = vi.fn().mockResolvedValue(undefined);
    const original = useResearchStore.getState().create;
    useResearchStore.setState({ selectedRunId: null, detail: null, create });
    try {
      await act(async () => { render(<ResearchWorkspace />); });
      const toggle = screen.getByRole("checkbox", { name: "精读时采集评论" });
      expect(toggle).not.toBeChecked();
      fireEvent.change(screen.getByLabelText("研究主题"), { target: { value: "软件使用体验" } });
      fireEvent.click(screen.getByRole("button", { name: "开始研究" }));
      await waitFor(() => expect(create).toHaveBeenLastCalledWith(expect.objectContaining({ includeComments: false })));
      fireEvent.click(toggle);
      fireEvent.click(screen.getByRole("button", { name: "开始研究" }));
      await waitFor(() => expect(create).toHaveBeenLastCalledWith(expect.objectContaining({ includeComments: true })));
      fireEvent.click(screen.getByRole("checkbox", { name: "抖音" }));
      fireEvent.click(screen.getByRole("checkbox", { name: "小红书" }));
      expect(screen.queryByRole("checkbox", { name: "精读时采集评论" })).toBeNull();
      fireEvent.click(screen.getByRole("button", { name: "开始研究" }));
      await waitFor(() => expect(create).toHaveBeenLastCalledWith(expect.objectContaining({ includeComments: false, sources: ["bilibili"] })));
    } finally { act(() => { useResearchStore.setState({ create: original }); }); }
  });
  it("综合列表带来源标识，固定标签可切换平台及查看失败原因", () => {
    render(<ResearchWorkspace />);
    expect(screen.getByRole("status")).toHaveTextContent("采集结束 · 部分平台异常");
    expect(screen.getByText("2 条候选 · 2/3 个平台有结果")).toBeInTheDocument();
    const panel = screen.getByRole("tabpanel");
    expect(within(panel).getByText("B 站视频候选")).toBeInTheDocument();
    expect(within(panel).getByText("抖音视频候选")).toBeInTheDocument();
    fireEvent.scroll(panel, { target: { scrollTop: 320 } });
    fireEvent.click(screen.getByRole("tab", { name: /抖音/ }));
    expect(panel.scrollTop).toBe(0);
    expect(screen.queryByText("B 站视频候选")).not.toBeInTheDocument();
    expect(screen.getByText("抖音视频候选")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("tab", { name: /小红书/ }));
    expect(within(panel).getByText(/平台搜索页加载超时/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("tab", { name: /综合/ }));
    expect(panel.scrollTop).toBe(320);
  });
  it("平台标签支持方向键切换，跨平台勾选不会丢失", () => {
    render(<ResearchWorkspace />);
    fireEvent.click(screen.getByRole("checkbox", { name: "选择 B 站视频候选" }));
    const all = screen.getByRole("tab", { name: /综合/ });
    fireEvent.keyDown(all, { key: "End" });
    expect(screen.getByRole("tab", { name: /小红书/ })).toHaveAttribute("aria-selected", "true");
    expect(screen.getByText("已选 1 条")).toBeInTheDocument();
    fireEvent.keyDown(screen.getByRole("tab", { name: /小红书/ }), { key: "Home" });
    expect(all).toHaveAttribute("aria-selected", "true");
    expect(screen.getByRole("checkbox", { name: "选择 B 站视频候选" })).toBeChecked();
  });
  it("运行中展示真实平台完成进度、当前页和阶段", () => {
    const detail = researchFixture();
    detail.run.status = "collecting";
    detail.run.completedAt = null;
    detail.sources[1].status = "running";
    detail.sources[1].progress = "第 1/3 页 · 正在等待平台返回搜索结果";
    detail.sources[2].status = "pending";
    detail.sources[2].error = null;
    render(<ResearchProgress detail={detail} selectedSource="all" onSelectSource={vi.fn()} />);
    expect(screen.getByRole("progressbar")).toHaveAttribute("aria-valuenow", "1");
    expect(screen.getByRole("progressbar")).toHaveAttribute("aria-valuemax", "3");
    expect(screen.getByText("第 1/3 页 · 正在等待平台返回搜索结果")).toBeInTheDocument();
    expect(screen.getByText("等待可用的采集窗口")).toBeInTheDocument();
  });
  it("验证按钮提交原研究及单个平台，恢复由主进程持续执行", async () => {
    const verifyAndRetrySource = vi.fn().mockResolvedValue({ status: "collecting" });
    const clone = vi.fn();
    installWindowMocks({ api: { research: { verifyAndRetrySource, clone } } });
    const detail = researchFixture();
    detail.sources[1].status = "failed";
    detail.sources[1].errorCode = "verification_required";
    detail.sources[1].error = "抖音搜索页要求安全验证";
    render(<ResearchProgress detail={detail} selectedSource="all" onSelectSource={vi.fn()} />);
    fireEvent.click(screen.getByRole("button", { name: "打开抖音搜索页验证" }));
    await waitFor(() => expect(verifyAndRetrySource).toHaveBeenCalledWith("run-1", "douyin"));
    expect(clone).not.toHaveBeenCalled();
    expect(screen.getAllByText("验证通过后自动补采当前平台").length).toBeGreaterThan(0);
  });
  it("验证启动失败会反馈错误，报告生成期间禁用验证补采入口", async () => {
    installWindowMocks({ api: { research: { verifyAndRetrySource: vi.fn().mockRejectedValue(new Error("研究仍在执行")) } } });
    const detail = researchFixture();
    detail.sources[1].error = "需要验证";
    const view = render(<ResearchProgress detail={detail} selectedSource="all" onSelectSource={vi.fn()} />);
    fireEvent.click(screen.getByRole("button", { name: "打开抖音搜索页验证" }));
    await waitFor(() => expect(showToast).toHaveBeenCalledWith("无法开始验证并补采", "error", { detail: "研究仍在执行" }));
    detail.run.reportStatus = "generating";
    view.rerender(<ResearchProgress detail={detail} selectedSource="all" onSelectSource={vi.fn()} />);
    expect(screen.getByRole("button", { name: "打开抖音搜索页验证" })).toBeDisabled();
  });
  it("读取失败展示重试，不会无限转圈", () => {
    useResearchStore.setState({ detail: null, error: "数据库读取失败" });
    render(<ResearchWorkspace />);
    expect(screen.getByRole("alert")).toHaveTextContent("数据库读取失败");
    expect(screen.getByRole("button", { name: /重试|Retry/ })).toBeInTheDocument();
  });
});
