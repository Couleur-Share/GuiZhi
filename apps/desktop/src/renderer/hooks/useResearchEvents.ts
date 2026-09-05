import { useEffect } from "react";
import { useResearchStore } from "../stores/research.store";
import { useToast } from "../components/ui/Toast";
import { researchSummary, RUN_STATUS_NAMES, SOURCE_NAMES } from "../components/research/research-presentation";
import { isWebRuntime } from "../runtime";

/** 在应用根部订阅，离开研究页也能收到一次完成反馈。 */
export function useResearchEvents(): void {
  const { showToast } = useToast();
  useEffect(() => {
    if (isWebRuntime()) return;
    const store = useResearchStore.getState();
    const unsubscribe = store.subscribeChanges((detail) => {
      const status = detail.run.status;
      showToast(`${detail.run.topic}：${RUN_STATUS_NAMES[status]}，${researchSummary(detail)}`, status === "ready" ? "success" : status === "failed" ? "error" : status === "canceled" ? "info" : "warning", {
        detail: detail.sources.filter((source) => source.error).map((source) => `${SOURCE_NAMES[source.source]}：${source.error}`).join("\n") || undefined,
      });
    });
    void store.refresh();
    return unsubscribe;
  }, [showToast]);
}
