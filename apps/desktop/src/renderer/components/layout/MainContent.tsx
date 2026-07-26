import { Suspense, useEffect } from "react";
import { useUIStore } from "../../stores/ui.store";
import { Spinner } from "../ui/Spinner";
import { MODULE_WORKSPACES, prefetchAppModules } from "./module-chunks";

const loadingFallback = (
  <div className="delayed-fade-in flex flex-1 items-center justify-center">
    <Spinner />
  </div>
);

export function MainContent() {
  const appModule = useUIStore((state) => state.appModule);
  const Workspace = MODULE_WORKSPACES[appModule];

  // 其余模块的 chunk 在空闲时段先拉好。等用户点了导航再加载，切换那一瞬间
  // Suspense 的转圈就会闪一下——这是「首次进入某个模块才卡一下」的来源。
  useEffect(() => {
    if (typeof window.requestIdleCallback !== "function") {
      prefetchAppModules();
      return;
    }
    // 一直没有空闲时段就 2 秒后强制执行，别让预取无限期推迟
    const handle = window.requestIdleCallback(prefetchAppModules, {
      timeout: 2000,
    });
    return () => window.cancelIdleCallback(handle);
  }, []);

  return (
    <Suspense fallback={loadingFallback}>
      <Workspace />
    </Suspense>
  );
}
