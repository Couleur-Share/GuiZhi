import { renderHook, act, waitFor } from "@testing-library/react";
import { beforeEach, it, expect, vi } from "vitest";
import { installWindowMocks } from "../../helpers/window";
import { useThemedReading } from "../../../src/renderer/components/themed-reading/use-themed-reading";
vi.mock("../../../src/renderer/components/ui/Toast", () => ({
  useToast: () => ({ showToast: vi.fn() }),
}));
vi.mock("react-i18next", async (importOriginal) => ({
  ...await importOriginal<typeof import("react-i18next")>(),
  useTranslation: () => ({ t: (_key, value) => value }),
}));
let receive: (task: any) => void;
beforeEach(() =>
  installWindowMocks({
    api: {
      themedReading: {
        get: vi.fn(),
        onProgress: vi.fn((fn) => {
          receive = fn;
          return () => {};
        }),
      },
    },
  }),
);
it("完成事件保留当前阅读版，主动切换后才展示新版", async () => {
  const task = {
    id: "task",
    itemId: "one",
    sourceKind: "body",
    state: "running",
  };
  vi.mocked(window.api.themedReading.get).mockResolvedValue({
    success: true,
    page: { id: "old", itemId: "one" } as any,
    task: task as any,
  });
  const { result } = renderHook(() => useThemedReading("one", "body", "正文"));
  await waitFor(() => expect(result.current.result?.page?.id).toBe("old"));
  vi.mocked(window.api.themedReading.get).mockResolvedValue({
    success: true,
    page: { id: "new", itemId: "one" } as any,
  });
  act(() => receive({ ...task, state: "completed" }));
  await waitFor(() => expect(result.current.latest?.page?.id).toBe("new"));
  expect(result.current.result.page.id).toBe("old");
  act(() => result.current.adoptLatest());
  expect(result.current.result.page.id).toBe("new");
});
it("条目切换清除旧版固定状态，迟到事件不能覆盖新条目", async () => {
  vi.mocked(window.api.themedReading.get).mockResolvedValue({
    success: true,
    page: { id: "old", itemId: "one" } as any,
    task: { itemId: "one", state: "running" } as any,
  });
  const { result, rerender } = renderHook(
    ({ id }) => useThemedReading(id, "body", "正文"),
    { initialProps: { id: "one" } },
  );
  await waitFor(() => expect(result.current.result?.page?.id).toBe("old"));
  vi.mocked(window.api.themedReading.get).mockResolvedValue({
    success: true,
    page: { id: "two-page", itemId: "two" } as any,
  });
  rerender({ id: "two" });
  await waitFor(() => expect(result.current.result?.page?.id).toBe("two-page"));
  act(() =>
    receive({
      id: "old-task",
      itemId: "one",
      sourceKind: "body",
      state: "completed",
    }),
  );
  expect(result.current.result.page.id).toBe("two-page");
  expect(result.current.latest).toBeNull();
});
