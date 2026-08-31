import { useEffect } from "react";
import type { BackgroundJob } from "@guizhi/shared/types";
import { IPC_CHANNELS } from "@guizhi/shared/constants/ipc-channels";
import { useSettingsStore } from "../stores/settings.store";
import { isSemanticConfigured } from "../services/knowledge-ai/embeddings";
import { isWebRuntime } from "../runtime";

const HEARTBEAT_INTERVAL_MS = 30_000;

function isRendererJob(value: unknown): value is BackgroundJob {
  if (!value || typeof value !== "object") return false;
  const job = value as Partial<BackgroundJob>;
  return (
    typeof job.id === "string" &&
    (job.kind === "wiki-compile" || job.kind === "semantic-index")
  );
}

async function executeRendererJob(job: BackgroundJob): Promise<void> {
  if (job.kind === "wiki-compile") {
    const { runBackgroundWikiCompile } = await import("../stores/wiki.store");
    await runBackgroundWikiCompile({ rethrow: true });
    return;
  }

  const { useSemanticStore } = await import("../stores/semantic.store");
  const result = await useSemanticStore.getState().runIndexing(true);
  if (result?.kind === "failed" || result?.kind === "partial") {
    throw new Error(result.message || `语义索引失败 ${result.failed} 项`);
  }
}

/**
 * 隐藏窗口也会保持挂载的 Renderer 后台客户端。调度权与重试状态均在主进程，
 * 此处只执行依赖浏览器侧 AI 配置的 Wiki/语义任务，并定期续租。
 */
export function useBackgroundJobClient(): void {
  const wikiCompileEnabled = useSettingsStore(
    (state) => state.wikiCompileEnabled,
  );
  const backgroundTasksEnabled = useSettingsStore(
    (state) => state.backgroundTasksEnabled,
  );
  // 订阅这两份配置使 embedding 路由变更后能够重新同步开关。
  const aiModels = useSettingsStore((state) => state.aiModels);
  const modelRouteDefaults = useSettingsStore(
    (state) => state.modelRouteDefaults,
  );

  useEffect(() => {
    if (isWebRuntime() || !window.api?.backgroundJob) return;
    void window.api.backgroundJob.syncRenderer({
      wikiEnabled: backgroundTasksEnabled && wikiCompileEnabled,
      semanticEnabled: backgroundTasksEnabled && isSemanticConfigured(),
    });
  }, [backgroundTasksEnabled, wikiCompileEnabled, aiModels, modelRouteDefaults]);

  useEffect(() => {
    if (isWebRuntime() || !window.api?.backgroundJob) return;
    let disposed = false;
    const handleAvailable = (value: unknown) => {
      if (!isRendererJob(value) || disposed) return;
      const heartbeat = setInterval(() => {
        void window.api.backgroundJob.renew(value.id);
      }, HEARTBEAT_INTERVAL_MS);
      void executeRendererJob(value)
        .then(() => window.api.backgroundJob.complete(value.id))
        .catch((error) =>
          window.api.backgroundJob.fail({
            id: value.id,
            error: error instanceof Error ? error.message : String(error),
          }),
        )
        .finally(() => clearInterval(heartbeat));
    };
    window.api.on(IPC_CHANNELS.BACKGROUND_JOB_AVAILABLE, handleAvailable);
    return () => {
      disposed = true;
      window.api.off(IPC_CHANNELS.BACKGROUND_JOB_AVAILABLE, handleAvailable);
    };
  }, []);
}
