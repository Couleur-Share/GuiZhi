import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import { changeLanguage, i18nReady } from "../../../src/renderer/i18n";
import { SetupChecklistDialog } from "../../../src/renderer/components/setup/SetupChecklistDialog";
import { APP_NEW_ITEM_EVENT } from "../../../src/renderer/components/app/app-command-events";
import { useSettingsStore } from "../../../src/renderer/stores/settings.store";
import { useUIStore } from "../../../src/renderer/stores/ui.store";

beforeAll(async () => {
  await i18nReady;
  await changeLanguage("zh");
});

const configuredModel = {
  id: "chat-1",
  provider: "openai",
  apiProtocol: "openai",
  apiKey: "sk-test",
  apiUrl: "https://api.example.com/v1",
  model: "gpt-4o-mini",
  isDefault: true,
} as const;

function renderDialog() {
  const onClose = vi.fn();
  const onDismissPermanently = vi.fn();
  render(
    <SetupChecklistDialog
      isOpen
      onClose={onClose}
      onDismissPermanently={onDismissPermanently}
    />,
  );
  return { onClose, onDismissPermanently };
}

beforeEach(() => {
  localStorage.clear();
  useSettingsStore.setState({
    aiModels: [],
    modelRouteDefaults: undefined,
    aiProvider: "",
    aiApiKey: "",
    aiApiUrl: "",
    aiModel: "",
  });
});

describe("首启引导框", () => {
  it("未配置文本模型时只有一个主操作，可选项默认折叠，也不再要求安装 yt-dlp", async () => {
    renderDialog();

    expect(screen.getByRole("button", { name: "快速配置" })).toBeTruthy();
    expect(screen.queryByText("去配置")).toBeNull();
    expect(screen.queryByText(/yt-dlp/)).toBeNull();
    expect(screen.queryByRole("button", { name: "开始采集" })).toBeNull();

    const toggle = screen.getByRole("button", { name: /更多可选设置（2）/ });
    expect(toggle).toHaveAttribute("aria-expanded", "false");
    await userEvent.setup().click(toggle);
    expect(toggle).toHaveAttribute("aria-expanded", "true");
    expect(screen.getByText("让视频和音频转成文字")).toBeTruthy();
    expect(screen.getByText("配置语义检索")).toBeTruthy();
    // 界面文案不再暴露 audioText / embedding 这类内部术语
    expect(screen.queryByText(/audioText|embedding/i)).toBeNull();
  });

  it("点「快速配置」进入模型快速配置并关闭本会话引导，不写入已忽略标记", async () => {
    const requestAiQuickSetup = vi.fn();
    const requestSettingsSection = vi.fn();
    useUIStore.setState({ requestAiQuickSetup, requestSettingsSection });
    const { onClose, onDismissPermanently } = renderDialog();

    await userEvent.setup().click(screen.getByRole("button", { name: "快速配置" }));

    expect(requestAiQuickSetup).toHaveBeenCalledTimes(1);
    expect(requestSettingsSection).toHaveBeenCalledWith("ai");
    expect(onClose).toHaveBeenCalledTimes(1);
    expect(onDismissPermanently).not.toHaveBeenCalled();
    expect(localStorage.getItem("guizhi-setup-dismissed")).toBeNull();
  });

  it("文本模型就绪后主操作变为「开始采集」，打开采集框并不再提示", async () => {
    useSettingsStore.setState({ aiModels: [configuredModel] });
    const opened = vi.fn();
    window.addEventListener(APP_NEW_ITEM_EVENT, opened);
    const { onDismissPermanently } = renderDialog();

    expect(screen.queryByRole("button", { name: "快速配置" })).toBeNull();
    await userEvent.setup().click(screen.getByRole("button", { name: "开始采集" }));
    window.removeEventListener(APP_NEW_ITEM_EVENT, opened);

    await waitFor(() => expect(opened).toHaveBeenCalledTimes(1));
    expect(onDismissPermanently).toHaveBeenCalledTimes(1);
    expect(localStorage.getItem("guizhi-setup-dismissed")).toBe("1");
  });

  it("「稍后再说」写入已忽略标记", async () => {
    const { onDismissPermanently } = renderDialog();
    await userEvent.setup().click(screen.getByRole("button", { name: "稍后再说" }));
    expect(onDismissPermanently).toHaveBeenCalledTimes(1);
    expect(localStorage.getItem("guizhi-setup-dismissed")).toBe("1");
  });
});
