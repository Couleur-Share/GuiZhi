import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeAll, beforeEach, expect, it, vi } from "vitest";
import { WebVerificationRetryButton } from "../../../src/renderer/components/imports/WebVerificationRetryButton";
import { useImportStore } from "../../../src/renderer/stores/import.store";
import { changeLanguage, i18nReady } from "../../../src/renderer/i18n";
const retry = vi.fn();
beforeAll(async () => {
  await i18nReady;
  await changeLanguage("zh");
});
beforeEach(() => {
  vi.clearAllMocks();
  useImportStore.setState({ retryTask: retry });
});
it("主动验证通过守卫重试，成功入队才关闭详情", async () => {
  retry.mockResolvedValue(true);
  const close = vi.fn();
  render(<WebVerificationRetryButton taskId="task-1" onComplete={close} />);
  await userEvent.click(screen.getByRole("button", { name: "验证并自动采集" }));
  expect(retry).toHaveBeenCalledWith("task-1", { verifyWeb: true });
  await waitFor(() => expect(close).toHaveBeenCalledOnce());
});
it("入队失败保留详情，不误报成功", async () => {
  retry.mockResolvedValue(false);
  const close = vi.fn();
  render(<WebVerificationRetryButton taskId="task-1" onComplete={close} />);
  await userEvent.click(screen.getByRole("button", { name: "验证并自动采集" }));
  await waitFor(() => expect(screen.getByRole("button")).toBeEnabled());
  expect(close).not.toHaveBeenCalled();
});
