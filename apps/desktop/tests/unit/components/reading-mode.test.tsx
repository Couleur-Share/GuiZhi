import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { installWindowMocks } from "../../helpers/window";
import { useThemedReadingMode } from "../../../src/renderer/components/themed-reading/use-themed-reading-mode";

const listeners = new Set<
  (task: { itemId: string; sourceKind: string; state: string }) => void
>();
const getState = vi.fn();
beforeEach(() => {
  getState.mockReset();
  listeners.clear();
  installWindowMocks({
    api: {
      themedReading: {
        getState,
        onProgress: vi.fn((callback) => {
          listeners.add(callback);
          return () => listeners.delete(callback);
        }),
      },
    },
  });
});
const complete = (state = "completed") =>
  listeners.forEach((callback) =>
    callback({ itemId: "a", sourceKind: "body", state }),
  );

it("有成品默认 AI，晚到的首次查询不覆盖用户选中的原文", async () => {
  let resolve: (value: unknown) => void;
  getState.mockImplementationOnce(
    () =>
      new Promise((done) => {
        resolve = done;
      }),
  );
  const hook = renderHook(() => useThemedReadingMode("a", "body", true));
  act(() => hook.result.current.select(false));
  await act(async () => resolve({ success: true, state: { hasPage: true } }));
  expect(hook.result.current.active).toBe(false);
  hook.unmount();
  getState.mockResolvedValue({ success: true, state: { hasPage: true } });
  const reopened = renderHook(() => useThemedReadingMode("a", "body", true));
  await waitFor(() => expect(reopened.result.current.active).toBe(true));
});

it("后台生成完成保留当前原文、编辑与用户主动选择的 AI 阅读", async () => {
  getState.mockResolvedValue({ success: true, state: { hasPage: true } });
  const hook = renderHook(() => useThemedReadingMode("a", "body", true));
  await waitFor(() => expect(hook.result.current.active).toBe(true));
  act(() => hook.result.current.select(false));
  await act(async () => complete());
  expect(hook.result.current.active).toBe(false);
  act(() => hook.result.current.startEditing());
  await act(async () => complete());
  expect(hook.result.current.editing).toBe(true);
  expect(hook.result.current.active).toBe(false);
  act(() => hook.result.current.finishEditing());
  await act(async () => complete("partial"));
  expect(hook.result.current.active).toBe(false);
  act(() => hook.result.current.select(true));
  await act(async () => complete());
  expect(hook.result.current.active).toBe(true);
  expect(getState).toHaveBeenCalledTimes(1);
});

it("后台首次生成完成不会把正在阅读的原文自动切到 AI", async () => {
  getState.mockResolvedValue({ success: true, state: { hasPage: false } });
  const hook = renderHook(() => useThemedReadingMode("a", "body", true));
  await act(async () => {});
  getState.mockResolvedValue({ success: true, state: { hasPage: true } });
  await act(async () => complete());
  expect(hook.result.current.active).toBe(false);
  expect(getState).toHaveBeenCalledTimes(1);
});

it("上个条目的迟到初始查询不能覆盖当前条目", async () => {
  let resolve: (value: unknown) => void;
  getState.mockImplementationOnce(
    () =>
      new Promise((done) => {
        resolve = done;
      }),
  );
  getState.mockResolvedValue({ success: true, state: { hasPage: false } });
  const hook = renderHook(({ id }) => useThemedReadingMode(id, "body", true), {
    initialProps: { id: "a" },
  });
  hook.rerender({ id: "b" });
  await act(async () => resolve({ success: true, state: { hasPage: true } }));
  expect(hook.result.current.active).toBe(false);
});
