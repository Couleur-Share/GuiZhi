import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { PlatformAccountRows } from "../../../src/renderer/components/settings/capture/PlatformAccountRows";
import { ToastProvider } from "../../../src/renderer/components/ui/Toast";
import { installWindowMocks } from "../../helpers/window";
import { changeLanguage, i18nReady } from "../../../src/renderer/i18n";

function installPlatformMocks() {
  installWindowMocks({
    api: {
      platformCapture: {
        getStatuses: vi.fn().mockResolvedValue([
          { platform: "xiaohongshu", browser: "embedded", browserVersion: "126.0.0", available: true, loggedIn: true, busy: false },
          { platform: "douyin", browser: "embedded", browserVersion: "126.0.0", available: true, loggedIn: false, busy: false },
          { platform: "linuxdo", browser: "embedded", browserVersion: "126.0.0", available: true, loggedIn: false, busy: false },
        ]),
        login: vi.fn().mockResolvedValue({ platform: "douyin", browser: "embedded", available: true, loggedIn: true, busy: false }),
        cancelLogin: vi.fn().mockResolvedValue(true),
        logout: vi.fn().mockResolvedValue(undefined),
        clearAllData: vi.fn().mockResolvedValue(undefined),
        discoverCreator: vi.fn(),
        search: vi.fn(),
        cancelDiscovery: vi.fn(),
        listComments: vi.fn(),
        refreshComments: vi.fn(),
      },
    },
  });
}

beforeEach(async () => {
  installPlatformMocks();
  await i18nReady;
  await changeLanguage("zh");
});

describe("平台账号设置", () => {
  it("显示浏览器版本、登录状态与对应操作", async () => {
    render(<ToastProvider><PlatformAccountRows /></ToastProvider>);
    expect(await screen.findByText(/已登录 · 归知内置登录窗口 · Chromium 126\.0\.0/)).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "重新登录" }));
    await waitFor(() => expect(window.api.platformCapture.login)
      .toHaveBeenCalledWith("xiaohongshu", true));
    await userEvent.click(screen.getByRole("button", { name: "登录", exact: true }));
    await waitFor(() => expect(window.api.platformCapture.login)
      .toHaveBeenCalledWith("douyin", false));
  });

  it("清除全部登录数据必须经过确认", async () => {
    render(<ToastProvider><PlatformAccountRows /></ToastProvider>);
    await screen.findByText(/已登录/);
    await userEvent.click(screen.getByRole("button", { name: /清空|清除/ }));
    expect(screen.getByText(/不会影响归知数据库和你的日常浏览器/)).toBeInTheDocument();
    const clearButtons = screen.getAllByRole("button", { name: /清空|清除/ });
    await userEvent.click(clearButtons[clearButtons.length - 1]);
    await waitFor(() => expect(window.api.platformCapture.clearAllData).toHaveBeenCalledTimes(1));
    expect(await screen.findByText("平台登录数据已清除，小红书、抖音与 LINUX DO 均已退出"))
      .toBeInTheDocument();
  });
});
