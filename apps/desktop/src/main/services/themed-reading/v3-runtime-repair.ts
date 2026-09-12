import type { AIClientConfig } from "@guizhi/core";
import type { ThemedReadingVersion } from "@guizhi/shared/types/themed-reading";
import { callDesignModel } from "./design";
import { checkReadingScript, validateV3Page } from "./v3-document";

/** 运行检查只在隔离进程进行；故障模块最多修复一次，之后发布可读静态稿。 */
export async function repairReadingRuntime(
  page: ThemedReadingVersion,
  config: AIClientConfig | null,
  signal: AbortSignal,
  verify: (page: ThemedReadingVersion) => Promise<string | undefined>,
  save: () => void,
  request: () => void,
) {
  let failure = await verify(page);
  signal.throwIfAborted();
  if (!failure) return;
  const g = page.generation;
  const checkpoint = () => {
    if (g) g.scripts = page.design.scripts ?? [];
    save();
  };
  const active =
    page.design.scripts?.filter((s) => s.status !== "failed") ?? [];
  const identified = active.filter((s) =>
    failure.startsWith(`交互 ${s.id} 运行失败：`),
  );
  const modules = identified.length ? identified : active;
  for (const module of modules) {
    const issue = {
      kind: "runtime" as const,
      unit: module.id,
      message: failure.slice(0, 4000),
    };
    if (g) g.issues.push(issue);
    module.error = issue.message;
    checkpoint();
    if (g && !g.repairs[module.id] && config) {
      g.repairs[module.id] = 1;
      checkpoint();
      const result = await callDesignModel(
        config,
        JSON.stringify({
          task: "仅修复失败的经典JavaScript模块，返回{code:string}。保留原有交互，检查DOM选择器和库初始化；不要使用import/eval/Function/联网/存储/宿主能力，所有循环有界。",
          id: module.id,
          code: module.code,
          error: failure,
          html: page.design.html,
          libraries: page.design.libraries ?? [],
        }),
        signal,
        undefined,
        request,
      );
      signal.throwIfAborted();
      module.code = String(result.code ?? "");
      const syntax = checkReadingScript(module);
      module.status = syntax ? "failed" : "ready";
      module.error = syntax?.message;
      checkpoint();
    } else module.status = "failed";
  }
  validateV3Page(page);
  failure = await verify(page);
  signal.throwIfAborted();
  if (failure) {
    for (const module of active) {
      module.status = "failed";
      module.error = failure.slice(0, 4000);
    }
  } else if (g)
    g.issues = g.issues.filter(
      (i) =>
        !(
          i.kind === "runtime" &&
          modules.some((s) => s.id === i.unit && s.status === "ready")
        ),
    );
  checkpoint();
}
