import { beforeEach, describe, expect, it, vi } from "vitest";
import type { ResearchRunDetail } from "@guizhi/shared/types";
import { researchFixture } from "../../helpers/research";
import { installWindowMocks } from "../../helpers/window";
import { useResearchStore } from "../../../src/renderer/stores/research.store";

beforeEach(() => {
  installWindowMocks();
  useResearchStore.setState({ runs: [], selectedRunId: null, detail: null, error: null });
});
describe("研究变更订阅", () => {
  it("列表刷新期间的完成事件不会被旧的采集中快照覆盖", async () => {
    const detail = researchFixture();
    let resolveList!: (runs: ResearchRunDetail["run"][]) => void;
    let handler!: (value: ResearchRunDetail) => void;
    installWindowMocks({ api: { research: { list: vi.fn().mockImplementation(() => new Promise((resolve) => { resolveList = resolve; })) } } });
    vi.mocked(window.api.on).mockImplementation((_channel, listener) => { handler = listener as typeof handler; });
    const unsubscribe = useResearchStore.getState().subscribeChanges();
    const refreshing = useResearchStore.getState().refresh();
    handler(detail);
    resolveList([{ ...detail.run, status: "collecting" }]);
    await refreshing;
    expect(useResearchStore.getState().runs[0].status).toBe("partial");
    unsubscribe();
  });

  it("离开研究页也通知完成，重复事件和报告变更不重复提示", () => {
    let handler!: (detail: ResearchRunDetail) => void;
    vi.mocked(window.api.on).mockImplementation((_channel, listener) => { handler = listener as typeof handler; });
    const completed = vi.fn();
    const unsubscribe = useResearchStore.getState().subscribeChanges(completed);
    const detail = researchFixture();
    handler({ ...detail, run: { ...detail.run, status: "collecting" } });
    handler(detail);
    handler(detail);
    handler({ ...detail, run: { ...detail.run, reportStatus: "ready" } });
    expect(completed).toHaveBeenCalledTimes(1);
    expect(useResearchStore.getState().detail).toBeNull();
    unsubscribe();
    expect(window.api.off).toHaveBeenCalledWith("research:changed", handler);
  });
  it("切换研究后迟到的详情请求不能覆盖新选择", async () => {
    let resolveOld!: (detail: ResearchRunDetail) => void;
    const detail = researchFixture();
    installWindowMocks({ api: { research: { get: vi.fn().mockImplementationOnce(() => new Promise((resolve) => { resolveOld = resolve; })).mockResolvedValueOnce({ ...detail, run: { ...detail.run, id: "run-2" } }) } } });
    const old = useResearchStore.getState().select("run-1");
    await useResearchStore.getState().select("run-2");
    resolveOld(detail);
    await old;
    expect(useResearchStore.getState().detail?.run.id).toBe("run-2");
  });
  it("原研究补采再次完成只通知一次，切到其他研究也不抢回选择", () => {
    let handler!: (detail: ResearchRunDetail) => void;
    vi.mocked(window.api.on).mockImplementation((_channel, listener) => { handler = listener as typeof handler; });
    const completed = vi.fn();
    const unsubscribe = useResearchStore.getState().subscribeChanges(completed);
    const detail = researchFixture();
    handler(detail);
    useResearchStore.setState({ selectedRunId: "run-2", detail: { ...detail, run: { ...detail.run, id: "run-2" } } });
    handler({ ...detail, run: { ...detail.run, status: "collecting" } });
    const recovered = { ...detail, run: { ...detail.run, status: "ready" as const } };
    handler(recovered);
    handler(recovered);
    expect(completed).toHaveBeenCalledTimes(1);
    expect(completed).toHaveBeenCalledWith(recovered);
    expect(useResearchStore.getState().runs).toHaveLength(1);
    expect(useResearchStore.getState().detail?.run.id).toBe("run-2");
    unsubscribe();
  });
});
