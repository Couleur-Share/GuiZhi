import { beforeEach, describe, expect, it } from "vitest";
import {
  SIDEBAR_PANEL_WIDTH_MAX,
  SIDEBAR_PANEL_WIDTH_MIN,
  useUIStore,
} from "../../../src/renderer/stores/ui.store";

describe("useUIStore", () => {
  beforeEach(() => {
    useUIStore.setState({
      appModule: "library",
      isSidebarCollapsed: false,
      libraryViewMode: "card",
    });
  });

  it("默认模块为 library", () => {
    expect(useUIStore.getState().appModule).toBe("library");
  });

  it("setAppModule 接受归知的四个模块", () => {
    for (const moduleId of ["ask", "wiki", "imports", "library"] as const) {
      useUIStore.getState().setAppModule(moduleId);
      expect(useUIStore.getState().appModule).toBe(moduleId);
    }
  });

  it("setAppModule 将未知值归一化为 library", () => {
    useUIStore.getState().setAppModule("prompt" as never);
    expect(useUIStore.getState().appModule).toBe("library");
  });

  it("侧栏宽度被夹在合法范围内", () => {
    useUIStore.getState().setSidebarPanelWidth(1);
    expect(useUIStore.getState().sidebarPanelWidth).toBe(
      SIDEBAR_PANEL_WIDTH_MIN,
    );
    useUIStore.getState().setSidebarPanelWidth(99999);
    expect(useUIStore.getState().sidebarPanelWidth).toBe(
      SIDEBAR_PANEL_WIDTH_MAX,
    );
  });

  it("toggleSidebar 翻转折叠状态", () => {
    expect(useUIStore.getState().isSidebarCollapsed).toBe(false);
    useUIStore.getState().toggleSidebar();
    expect(useUIStore.getState().isSidebarCollapsed).toBe(true);
  });

  it("知识库视图默认卡片，未知值归一化", () => {
    expect(useUIStore.getState().libraryViewMode).toBe("card");
    useUIStore.getState().setLibraryViewMode("list");
    expect(useUIStore.getState().libraryViewMode).toBe("list");
    useUIStore.getState().setLibraryViewMode("gallery" as never);
    expect(useUIStore.getState().libraryViewMode).toBe("card");
  });

  it("旧持久化的行密度值迁移到新视图模式", () => {
    // compact 是 v0.3 早期写入的紧凑档，等价于现在的列表视图
    useUIStore.getState().setLibraryViewMode("compact" as never);
    expect(useUIStore.getState().libraryViewMode).toBe("list");
    useUIStore.getState().setLibraryViewMode("comfortable" as never);
    expect(useUIStore.getState().libraryViewMode).toBe("card");
  });
});
