import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeAll, beforeEach, expect, it, vi } from "vitest";
import { FlareSolverrSettings } from "../../../src/renderer/components/settings/FlareSolverrSettings";
import { useSettingsStore } from "../../../src/renderer/stores/settings.store";
import { changeLanguage, i18nReady } from "../../../src/renderer/i18n";
import { DEFAULT_FLARESOLVERR_SETTINGS } from "@guizhi/shared/utils/flaresolverr";
const save = vi.fn(),
  check = vi.fn();
beforeAll(async () => {
  await i18nReady;
  await changeLanguage("zh");
});
beforeEach(() => {
  vi.clearAllMocks();
  useSettingsStore.setState({
    flareSolverr: {
      ...DEFAULT_FLARESOLVERR_SETTINGS,
      enabled: true,
      connection: "ssh",
      sshHost: "gatewaysentry",
    },
  });
  window.api.settings.set = save;
  window.api.webCapture = { ...window.api.webCapture, checkSolver: check };
});
it("连接成功不隐式保存，保存成功才更新采集设置", async () => {
  check.mockResolvedValue({ ok: true, data: { connected: true } });
  save.mockResolvedValue(true);
  render(<FlareSolverrSettings />);
  await userEvent.clear(screen.getByRole("textbox", { name: "SSH 主机别名" }));
  await userEvent.type(
    screen.getByRole("textbox", { name: "SSH 主机别名" }),
    "test-server",
  );
  await userEvent.click(screen.getByRole("button", { name: "测试连接" }));
  await screen.findByText("服务连接成功；实际验证结果以采集任务为准");
  expect(save).not.toHaveBeenCalled();
  expect(useSettingsStore.getState().flareSolverr.sshHost).toBe(
    "gatewaysentry",
  );
  await userEvent.click(screen.getByRole("button", { name: "保存设置" }));
  await screen.findByText("设置已保存");
  expect(useSettingsStore.getState().flareSolverr.sshHost).toBe("test-server");
});
it("设置保存失败保留原值，不显示成功", async () => {
  save.mockRejectedValue(new Error("磁盘不可写"));
  render(<FlareSolverrSettings />);
  await userEvent.clear(screen.getByRole("textbox", { name: "SSH 主机别名" }));
  await userEvent.type(
    screen.getByRole("textbox", { name: "SSH 主机别名" }),
    "test-server",
  );
  await userEvent.click(screen.getByRole("button", { name: "保存设置" }));
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "保存设置" })).toBeEnabled(),
  );
  expect(useSettingsStore.getState().flareSolverr.sshHost).toBe(
    "gatewaysentry",
  );
  expect(screen.queryByText("设置已保存")).not.toBeInTheDocument();
});
