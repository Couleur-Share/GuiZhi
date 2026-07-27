/**
 * 配图风格按知识库记忆。
 *
 * styleId 是面板的组件状态，面板一关就卸载。不记的话每次打开都落回第一套——
 * 技术条目要蓝图、情感条目要小人，每篇都得重选一遍。
 */
import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import type { IllustrationStyle, KnowledgeItem } from "@guizhi/shared/types";
import { installWindowMocks } from "../../helpers/window";
import { ToastProvider } from "../../../src/renderer/components/ui/Toast";
import { useIllustrations } from "../../../src/renderer/components/library/use-illustrations";

const style = (id: string): IllustrationStyle => ({
  id,
  name: id,
  description: "",
  group: "",
  visualDna: "x",
  character: "",
  negative: "",
  aspectRatio: "16:9",
  maxShots: 4,
  maxLabels: 4,
});

const STYLES = [style("hand-note"), style("blueprint-dark")];

const item = (collectionId: string | null): KnowledgeItem =>
  ({
    id: "item-1",
    title: "条目",
    content: "正文",
    collectionId,
  }) as KnowledgeItem;

function wrapper({ children }: { children: ReactNode }) {
  return <ToastProvider>{children}</ToastProvider>;
}

function renderFor(collectionId: string | null, styles = STYLES) {
  installWindowMocks({
    api: {
      illustration: {
        styles: vi.fn().mockResolvedValue(styles),
        onProgress: vi.fn().mockReturnValue(() => {}),
      },
    },
  });
  return renderHook(() => useIllustrations(item(collectionId)), { wrapper });
}

beforeEach(() => {
  localStorage.clear();
});

describe("配图风格记忆", () => {
  it("没选过时落在第一套", async () => {
    const { result } = renderFor("col-ai");
    await waitFor(() => expect(result.current.styleId).toBe("hand-note"));
  });

  it("选过之后，同一知识库再打开就是上次那套", async () => {
    const first = renderFor("col-ai");
    await waitFor(() => expect(first.result.current.styleId).toBe("hand-note"));
    act(() => first.result.current.setStyleId("blueprint-dark"));
    first.unmount();

    const second = renderFor("col-ai");
    await waitFor(() =>
      expect(second.result.current.styleId).toBe("blueprint-dark"),
    );
  });

  it("各库各记，别的知识库不受影响", async () => {
    const ai = renderFor("col-ai");
    await waitFor(() => expect(ai.result.current.styleId).toBe("hand-note"));
    act(() => ai.result.current.setStyleId("blueprint-dark"));
    ai.unmount();

    const mood = renderFor("col-mood");
    await waitFor(() => expect(mood.result.current.styleId).toBe("hand-note"));
  });

  it("未归库的条目自成一格，不跟着任何知识库走", async () => {
    const ai = renderFor("col-ai");
    await waitFor(() => expect(ai.result.current.styleId).toBe("hand-note"));
    act(() => ai.result.current.setStyleId("blueprint-dark"));
    ai.unmount();

    const loose = renderFor(null);
    await waitFor(() => expect(loose.result.current.styleId).toBe("hand-note"));
  });

  // 记住的那套可能已经在编辑器里被删掉，不能留一个选不出名字的空选择
  it("记住的风格已被删除时落回第一套", async () => {
    const before = renderFor("col-ai");
    await waitFor(() => expect(before.result.current.styleId).toBe("hand-note"));
    act(() => before.result.current.setStyleId("blueprint-dark"));
    before.unmount();

    const after = renderFor("col-ai", [style("hand-note"), style("warm-life")]);
    await waitFor(() => expect(after.result.current.styleId).toBe("hand-note"));
  });
});
