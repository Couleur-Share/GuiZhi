import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import type { ImportTask } from "@guizhi/shared/types";
import { NodeseekRetryButton } from "../../../src/renderer/components/imports/NodeseekRetryButton";
import { ToastProvider } from "../../../src/renderer/components/ui/Toast";
import { useImportStore } from "../../../src/renderer/stores/import.store";
import { changeLanguage, i18nReady } from "../../../src/renderer/i18n";

const task = {
  id: "nodeseek-task",
  sourceInput: "https://www.nodeseek.com/post-123-1",
} as ImportTask;
const login = vi.fn();
const cancel = vi.fn();
const retry = vi.fn();
const complete = vi.fn();
function show() {
  return render(
    <ToastProvider>
      <NodeseekRetryButton task={task} onComplete={complete} />
    </ToastProvider>,
  );
}

describe("NodeSeek 验证自动重试", () => {
  beforeAll(async () => {
    await i18nReady;
    await changeLanguage("zh");
  });
  beforeEach(() => {
    login.mockReset();
    cancel.mockReset().mockResolvedValue(true);
    retry.mockReset().mockResolvedValue(true);
    complete.mockReset();
    window.api.platformCapture = { login, cancelLogin: cancel };
    useImportStore.setState({ retryTask: retry });
  });
  it("验证完成前不重试，完成后自动重试同一任务且只触发一次", async () => {
    let resolve!: () => void;
    login.mockReturnValue(
      new Promise<void>((done) => {
        resolve = done;
      }),
    );
    show();
    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: "验证并自动采集" }));
    expect(login).toHaveBeenCalledWith(
      "nodeseek",
      false,
      undefined,
      task.sourceInput,
    );
    expect(retry).not.toHaveBeenCalled();
    expect(
      screen.getByRole("button", { name: "等待网页验证…" }),
    ).toBeDisabled();
    resolve();
    await waitFor(() => expect(complete).toHaveBeenCalledOnce());
    expect(retry).toHaveBeenCalledOnce();
    expect(retry).toHaveBeenCalledWith(task.id, {
      captureStrategy: "authenticated",
    });
  });
  it("验证失败显示原因，保留重试入口", async () => {
    login.mockRejectedValue(new Error("验证窗口已关闭"));
    show();
    await userEvent
      .setup()
      .click(screen.getByRole("button", { name: "验证并自动采集" }));
    await screen.findByText("NodeSeek 验证未完成，可再次尝试");
    expect(retry).not.toHaveBeenCalled();
    expect(complete).not.toHaveBeenCalled();
    expect(
      screen.getByRole("button", { name: "验证并自动采集" }),
    ).toBeEnabled();
  });
  it("用户取消后，即使验证晚到也不自动重试", async () => {
    let resolve!: () => void;
    login.mockReturnValue(
      new Promise<void>((done) => {
        resolve = done;
      }),
    );
    show();
    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: "验证并自动采集" }));
    await user.click(screen.getByRole("button", { name: "取消" }));
    expect(cancel).toHaveBeenCalledWith("nodeseek");
    resolve();
    await screen.findByRole("button", { name: "验证并自动采集" });
    expect(retry).not.toHaveBeenCalled();
  });
  it("重试入队失败不会关闭详情或报成功", async () => {
    login.mockResolvedValue({ loggedIn: true });
    retry.mockResolvedValue(false);
    show();
    await userEvent
      .setup()
      .click(screen.getByRole("button", { name: "验证并自动采集" }));
    await waitFor(() => expect(retry).toHaveBeenCalledOnce());
    expect(complete).not.toHaveBeenCalled();
  });
});
