import { lazy, type ComponentType, type LazyExoticComponent } from "react";
import type { AppModule } from "../../stores/ui.store";

/**
 * 模块级代码分割的唯一出口。
 *
 * 每个模块拆成「工作区 + 侧栏面板」两个 chunk。首次点进 AI 问答 / Wiki /
 * 导入时要现拉两份，两个 Suspense fallback 各转一次圈再消失——这是首次
 * 进入会闪一下的原因之一。loader 单独留出来供预取调用：预取与 lazy 用的是
 * 同一个动态 import 说明符，ESM 的模块缓存保证只会真正加载一次。
 */
const MODULE_LOADERS = {
  library: {
    workspace: () => import("../library/LibraryWorkspace"),
    panel: () => import("../library/SidebarLibraryPanel"),
  },
  inbox: {
    workspace: () => import("../inbox/InboxWorkspace"),
    panel: () => import("../inbox/SidebarInboxPanel"),
  },
  ask: {
    workspace: () => import("../ask/AskWorkspace"),
    panel: () => import("../ask/SidebarAskPanel"),
  },
  wiki: {
    workspace: () => import("../wiki/WikiWorkspace"),
    panel: () => import("../wiki/SidebarWikiPanel"),
  },
  research: {
    workspace: () => import("../research/ResearchWorkspace"),
    panel: () => import("../research/SidebarResearchPanel"),
  },
  imports: {
    workspace: () => import("../imports/ImportsWorkspace"),
    panel: () => import("../imports/SidebarImportsPanel"),
  },
};

export const MODULE_WORKSPACES: Record<
  AppModule,
  LazyExoticComponent<ComponentType>
> = {
  library: lazy(() =>
    MODULE_LOADERS.library
      .workspace()
      .then((module) => ({ default: module.LibraryWorkspace })),
  ),
  inbox: lazy(() =>
    MODULE_LOADERS.inbox
      .workspace()
      .then((module) => ({ default: module.InboxWorkspace })),
  ),
  ask: lazy(() =>
    MODULE_LOADERS.ask
      .workspace()
      .then((module) => ({ default: module.AskWorkspace })),
  ),
  wiki: lazy(() =>
    MODULE_LOADERS.wiki
      .workspace()
      .then((module) => ({ default: module.WikiWorkspace })),
  ),
  research: lazy(() =>
    MODULE_LOADERS.research
      .workspace()
      .then((module) => ({ default: module.ResearchWorkspace })),
  ),
  imports: lazy(() =>
    MODULE_LOADERS.imports
      .workspace()
      .then((module) => ({ default: module.ImportsWorkspace })),
  ),
};

export const MODULE_PANELS: Record<
  AppModule,
  LazyExoticComponent<ComponentType>
> = {
  library: lazy(() =>
    MODULE_LOADERS.library
      .panel()
      .then((module) => ({ default: module.SidebarLibraryPanel })),
  ),
  inbox: lazy(() =>
    MODULE_LOADERS.inbox
      .panel()
      .then((module) => ({ default: module.SidebarInboxPanel })),
  ),
  ask: lazy(() =>
    MODULE_LOADERS.ask
      .panel()
      .then((module) => ({ default: module.SidebarAskPanel })),
  ),
  wiki: lazy(() =>
    MODULE_LOADERS.wiki
      .panel()
      .then((module) => ({ default: module.SidebarWikiPanel })),
  ),
  research: lazy(() =>
    MODULE_LOADERS.research
      .panel()
      .then((module) => ({ default: module.SidebarResearchPanel })),
  ),
  imports: lazy(() =>
    MODULE_LOADERS.imports
      .panel()
      .then((module) => ({ default: module.SidebarImportsPanel })),
  ),
};

/**
 * 预热全部模块 chunk，应用挂载后在空闲时段调用一次。
 *
 * 预取失败不做任何处理：真正切过去时 lazy 会自己再走一遍并把错误交给上层
 * 错误边界，这里只是不想留下未处理的 rejection。
 */
export function prefetchAppModules(): void {
  for (const loaders of Object.values(MODULE_LOADERS)) {
    void loaders.workspace().catch(() => {});
    void loaders.panel().catch(() => {});
  }
}
