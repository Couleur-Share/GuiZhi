import { act, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { installWindowMocks } from "../../helpers/window";
import { changeLanguage, i18nReady } from "../../../src/renderer/i18n";
import { ToastProvider } from "../../../src/renderer/components/ui/Toast";
import { CaptureSection } from "../../../src/renderer/components/settings/capture/CaptureSection";
import { clearEngineStatusCache } from "../../../src/renderer/components/settings/capture/use-engine-status";

const YTDLP_MANAGED = {
  installed: true,
  source: "managed" as const,
  version: "2026.07.04",
  path: "C:/Users/test/AppData/Roaming/GuiZhi/tools/yt-dlp.exe",
  managedPath: "C:/Users/test/AppData/Roaming/GuiZhi/tools/yt-dlp.exe",
  installSupported: true,
};

// 与用户截图一致：ffmpeg 来自系统 PATH，应用没有托管副本可移除
const FFMPEG_ON_PATH = {
  installed: true,
  source: "path" as const,
  version: "8.1-essentials_build-www.gyan.dev",
  path: "ffmpeg",
  managedPath: "C:/Users/test/AppData/Roaming/GuiZhi/tools/ffmpeg.exe",
  installSupported: true,
};

const FUNASR_INSTALLED = {
  installed: true,
  running: false,
  port: 8620,
  dir: "C:/Users/test/AppData/Roaming/GuiZhi/tools/funasr",
  version: "1.3.29",
  installSupported: true,
};

function installEngineMocks() {
  installWindowMocks({
    api: {
      ytdlp: {
        status: vi.fn().mockResolvedValue(YTDLP_MANAGED),
        checkUpdate: vi.fn().mockResolvedValue({
          current: YTDLP_MANAGED.version,
          latest: YTDLP_MANAGED.version,
          updateAvailable: false,
        }),
        install: vi.fn(),
        remove: vi.fn().mockResolvedValue(true),
        pickBinary: vi.fn().mockResolvedValue(null),
      },
      ffmpeg: {
        status: vi.fn().mockResolvedValue(FFMPEG_ON_PATH),
        checkUpdate: vi.fn().mockResolvedValue({
          current: "20260724",
          latest: "20260724",
          updateAvailable: false,
        }),
        install: vi.fn(),
        remove: vi.fn().mockResolvedValue(true),
        pickBinary: vi.fn().mockResolvedValue(null),
      },
      funasr: {
        status: vi.fn().mockResolvedValue(FUNASR_INSTALLED),
        checkUpdate: vi.fn().mockResolvedValue({ current: "1.3.29", latest: "1.3.30", updateAvailable: true }),
        update: vi.fn().mockResolvedValue({ success: true, version: "1.3.30" }),
        install: vi.fn().mockResolvedValue({ success: true }),
        uninstall: vi.fn().mockResolvedValue({ success: true }),
      },
    },
  });
}

function renderSection() {
  return render(
    <ToastProvider>
      <CaptureSection />
    </ToastProvider>,
  );
}

function row(engineId: string) {
  return within(screen.getByTestId(`capture-engine-${engineId}`));
}

beforeEach(async () => {
  clearEngineStatusCache();
  installEngineMocks();
  // jsdom 的 navigator.language 决定初始语言，显式切到中文让断言与文案对齐
  await i18nReady;
  await changeLanguage("zh");
});

describe("采集区", () => {
  /**
   * 探测出结果之前不知道装没装，此时给「一键安装」是在瞎猜——
   * 已装好的用户首次进设置页会看到一个诱导重装的实心主按钮。
   */
  it("探测中不摆出任何安装/更新动作", async () => {
    let resolveStatus: (value: unknown) => void = () => {};
    window.api.ytdlp.status.mockReturnValue(
      new Promise((resolve) => {
        resolveStatus = resolve;
      }),
    );
    window.api.funasr.status.mockReturnValue(new Promise(() => {}));
    renderSection();

    expect(row("ytdlp").getByText("检测中")).toBeInTheDocument();
    expect(row("ytdlp").queryByRole("button", { name: "一键安装" })).not.toBeInTheDocument();
    expect(row("ytdlp").queryByRole("button", { name: "检查更新" })).not.toBeInTheDocument();
    expect(row("funasr").queryByRole("button", { name: "一键安装" })).not.toBeInTheDocument();

    resolveStatus(YTDLP_MANAGED);
    await waitFor(() =>
      expect(row("ytdlp").getByRole("button", { name: "检查更新" })).toBeInTheDocument(),
    );
  });

  it("确认未安装后才出现一键安装", async () => {
    window.api.ytdlp.status.mockResolvedValue({
      installed: false,
      source: null,
      managedPath: "C:/tools/yt-dlp.exe",
    });
    renderSection();

    await waitFor(() =>
      expect(row("ytdlp").getByRole("button", { name: "一键安装" })).toBeInTheDocument(),
    );
  });

  /** ffmpeg 每日构建的原始版本串是一长条 git hash，直接铺在状态行里没法读 */
  it("ffmpeg 每日构建只展示构建日期，发行版只展示版本号", async () => {
    window.api.ffmpeg.status.mockResolvedValue({
      ...FFMPEG_ON_PATH,
      source: "managed",
      version: "N-125753-g6095372a70-20260724",
    });
    const nightly = renderSection();
    await waitFor(() =>
      expect(row("ffmpeg").getByText(/2026-07-24 · 内置版/)).toBeInTheDocument(),
    );
    expect(row("ffmpeg").queryByText(/g6095372a70/)).not.toBeInTheDocument();
    nightly.unmount();

    clearEngineStatusCache();
    window.api.ffmpeg.status.mockResolvedValue(FFMPEG_ON_PATH);
    renderSection();
    await waitFor(() =>
      expect(row("ffmpeg").getByText(/8\.1 · 系统 PATH/)).toBeInTheDocument(),
    );
  });

  /**
   * 状态行原本把「用途 + 版本 + 来源」全塞进一行，装好之后用途就是噪音。
   * 现在只在未安装时讲「为什么值得装」，装好后让位给版本与来源。
   */
  it("装好后状态行只留短用途 + 版本 + 来源", async () => {
    renderSection();
    await waitFor(() => expect(row("ytdlp").getByText("已就绪")).toBeInTheDocument());

    expect(
      row("ytdlp").getByText("在线视频解析 · 2026-07-04 · 内置版"),
    ).toBeInTheDocument();
    expect(row("ytdlp").queryByText(/B 站/)).not.toBeInTheDocument();
  });

  it("未安装时才补上「为什么值得装」", async () => {
    window.api.ytdlp.status.mockResolvedValue({
      installed: false,
      source: null,
      managedPath: "C:/tools/yt-dlp.exe",
    });
    renderSection();

    await waitFor(() =>
      expect(
        row("ytdlp").getByText(
          "在线视频解析 · B 站 / YouTube 等站点的视频导入依赖它",
        ),
      ).toBeInTheDocument(),
    );
  });

  it("本地转写引擎待命时不赘述，只有真在跑才标运行中", async () => {
    const idle = renderSection();
    await waitFor(() => expect(row("funasr").getByText("已就绪")).toBeInTheDocument());
    expect(row("funasr").getByText("SenseVoice 模型 · v1.3.29")).toBeInTheDocument();
    idle.unmount();

    clearEngineStatusCache();
    window.api.funasr.status.mockResolvedValue({
      ...FUNASR_INSTALLED,
      running: true,
    });
    renderSection();

    await waitFor(() =>
      expect(
        row("funasr").getByText("SenseVoice 模型 · v1.3.29 · 运行中"),
      ).toBeInTheDocument(),
    );
  });

  it("三个引擎探测完成后都显示已就绪", async () => {
    renderSection();

    for (const engineId of ["ytdlp", "ffmpeg", "funasr"]) {
      await waitFor(() =>
        expect(row(engineId).getByText("已就绪")).toBeInTheDocument(),
      );
    }
  });

  /** 用户反馈 3：自定义路径不需要时一直占着位置 */
  it("自定义路径默认收起，展开高级选项后才出现", async () => {
    const user = userEvent.setup();
    renderSection();
    await waitFor(() => expect(row("ytdlp").getByText("已就绪")).toBeInTheDocument());

    expect(row("ytdlp").queryByLabelText("自定义路径")).not.toBeInTheDocument();

    await user.click(row("ytdlp").getByLabelText("高级选项"));

    expect(row("ytdlp").getByLabelText("自定义路径")).toBeInTheDocument();
    expect(row("ytdlp").getByText("生效路径")).toBeInTheDocument();
  });

  /** 用户反馈 2：三个引擎的销毁入口位置与样式要一致 */
  it("移除 / 卸载都收在各自的高级面板里", async () => {
    const user = userEvent.setup();
    renderSection();
    await waitFor(() => expect(row("funasr").getByText("已就绪")).toBeInTheDocument());

    await user.click(row("ytdlp").getByLabelText("高级选项"));
    await user.click(row("funasr").getByLabelText("高级选项"));

    expect(
      row("ytdlp").getByRole("button", { name: "移除内置版" }),
    ).toBeInTheDocument();
    expect(
      row("funasr").getByRole("button", { name: "卸载引擎" }),
    ).toBeInTheDocument();
  });

  it("来源是系统 PATH 时没有可移除的内置副本", async () => {
    const user = userEvent.setup();
    renderSection();
    await waitFor(() => expect(row("ffmpeg").getByText("已就绪")).toBeInTheDocument());

    expect(row("ffmpeg").getByText(/· 系统 PATH$/)).toBeInTheDocument();

    await user.click(row("ffmpeg").getByLabelText("高级选项"));

    expect(
      row("ffmpeg").queryByRole("button", { name: "移除内置版" }),
    ).not.toBeInTheDocument();
    // PATH 来源只有命令名，没有可展示的生效路径
    expect(row("ffmpeg").queryByText("生效路径")).not.toBeInTheDocument();
  });

  it("移除内置版会先弹确认框，确认后才真正调用", async () => {
    const user = userEvent.setup();
    renderSection();
    await waitFor(() => expect(row("ytdlp").getByText("已就绪")).toBeInTheDocument());

    await user.click(row("ytdlp").getByLabelText("高级选项"));
    await user.click(row("ytdlp").getByRole("button", { name: "移除内置版" }));

    expect(window.api.ytdlp.remove).not.toHaveBeenCalled();

    const dialog = within(screen.getByRole("alertdialog"));
    await user.click(dialog.getByRole("button", { name: "移除内置版" }));

    await waitFor(() => expect(window.api.ytdlp.remove).toHaveBeenCalledTimes(1));
  });

  /** 用户反馈 1：每次进入设置页都要转圈检测一会 */
  it("重新进入设置页直接显示上次结果，不再回到检测中", async () => {
    const first = renderSection();
    await waitFor(() => expect(row("ytdlp").getByText("已就绪")).toBeInTheDocument());
    first.unmount();

    renderSection();
    expect(row("ytdlp").getByText("已就绪")).toBeInTheDocument();
    expect(screen.queryByText("检测中")).not.toBeInTheDocument();

    // 后台仍会静默复核一次，等它落地再结束用例
    await waitFor(() =>
      expect(window.api.ytdlp.status).toHaveBeenCalledTimes(2),
    );
  });

  /**
   * 用户反馈：既然能判定是最新版，就不该一直摆着「更新内置版」。
   * 对齐「关于应用」里应用自身更新入口的四态机：先检查，有更新才给更新按钮。
   */
  it("内置版默认给的是「检查更新」，不是「更新」", async () => {
    renderSection();
    await waitFor(() => expect(row("ytdlp").getByText("已就绪")).toBeInTheDocument());

    expect(
      row("ytdlp").getByRole("button", { name: "检查更新" }),
    ).toBeInTheDocument();
    expect(window.api.ytdlp.checkUpdate).not.toHaveBeenCalled();
    expect(row("ytdlp").queryByText(/更新到 v/)).not.toBeInTheDocument();
  });

  it("检查后没有更新 → 不再显示任何更新按钮，只留「已是最新」", async () => {
    const user = userEvent.setup();
    renderSection();
    await waitFor(() => expect(row("ytdlp").getByText("已就绪")).toBeInTheDocument());

    await user.click(row("ytdlp").getByRole("button", { name: "检查更新" }));

    await waitFor(() =>
      expect(row("ytdlp").getByText("已是最新")).toBeInTheDocument(),
    );
    expect(row("ytdlp").queryByRole("button", { name: "检查更新" })).not.toBeInTheDocument();
    expect(row("ytdlp").queryByText(/更新到 v/)).not.toBeInTheDocument();
    // 全程没有触发下载
    expect(window.api.ytdlp.install).not.toHaveBeenCalled();
  });

  it("检查到新版本 → 按钮变成带版本号的更新动作，点击才下载", async () => {
    const user = userEvent.setup();
    window.api.ytdlp.checkUpdate.mockResolvedValue({
      current: "2026.07.04",
      latest: "2026.07.20",
      updateAvailable: true,
    });
    window.api.ytdlp.install.mockResolvedValue({
      success: true,
      version: "2026.07.20",
    });
    window.api.ytdlp.status
      .mockResolvedValueOnce(YTDLP_MANAGED)
      .mockResolvedValue({ ...YTDLP_MANAGED, version: "2026.07.20" });
    renderSection();
    await waitFor(() => expect(row("ytdlp").getByText("已就绪")).toBeInTheDocument());

    await user.click(row("ytdlp").getByRole("button", { name: "检查更新" }));
    const updateButton = await waitFor(() =>
      row("ytdlp").getByRole("button", { name: "更新到 2026-07-20" }),
    );
    expect(window.api.ytdlp.install).not.toHaveBeenCalled();

    await user.click(updateButton);

    await waitFor(() =>
      expect(screen.getByText(/yt-dlp 安装完成（2026-07-20）/)).toBeInTheDocument(),
    );
    expect(row("ytdlp").getByText(/2026-07-20 · 内置版/)).toBeInTheDocument();
  });

  it("检查失败时退回可重试状态并给出提示", async () => {
    const user = userEvent.setup();
    window.api.ytdlp.checkUpdate.mockResolvedValue({
      current: "2026.07.04",
      latest: null,
      updateAvailable: false,
    });
    renderSection();
    await waitFor(() => expect(row("ytdlp").getByText("已就绪")).toBeInTheDocument());

    await user.click(row("ytdlp").getByRole("button", { name: "检查更新" }));

    await waitFor(() =>
      expect(screen.getByText("检查更新失败，请稍后重试")).toBeInTheDocument(),
    );
    expect(
      row("ytdlp").getByRole("button", { name: "检查更新" }),
    ).toBeInTheDocument();
  });

  /** ffmpeg 的可比标识是构建日期，按钮要说人话而不是「更新到 v20260725」 */
  it("ffmpeg 内置版同样先检查，有新构建才给更新按钮并按日期展示", async () => {
    const user = userEvent.setup();
    window.api.ffmpeg.status.mockResolvedValue({
      ...FFMPEG_ON_PATH,
      source: "managed",
      version: "N-125753-g6095372a70-20260724",
      path: "C:/Users/test/AppData/Roaming/GuiZhi/tools/ffmpeg.exe",
    });
    window.api.ffmpeg.checkUpdate.mockResolvedValue({
      current: "20260724",
      latest: "20260725",
      updateAvailable: true,
    });
    renderSection();
    await waitFor(() => expect(row("ffmpeg").getByText("已就绪")).toBeInTheDocument());

    await user.click(row("ffmpeg").getByRole("button", { name: "检查更新" }));

    await waitFor(() =>
      expect(
        row("ffmpeg").getByRole("button", { name: "更新到 2026-07-25" }),
      ).toBeInTheDocument(),
    );
    expect(window.api.ffmpeg.install).not.toHaveBeenCalled();
  });

  it("安装提示里的版本号与状态行同一套格式，不露出 git hash", async () => {
    const user = userEvent.setup();
    window.api.ffmpeg.install.mockResolvedValue({
      success: true,
      version: "N-125753-g6095372a70-20260724",
    });
    renderSection();
    await waitFor(() => expect(row("ffmpeg").getByText("已就绪")).toBeInTheDocument());

    await user.click(row("ffmpeg").getByRole("button", { name: "安装内置版" }));

    await waitFor(() =>
      expect(
        screen.getByText(/ffmpeg 安装完成（2026-07-24）/),
      ).toBeInTheDocument(),
    );
  });

  it("ffmpeg 已是最新构建时同样不留更新按钮", async () => {
    const user = userEvent.setup();
    window.api.ffmpeg.status.mockResolvedValue({
      ...FFMPEG_ON_PATH,
      source: "managed",
      version: "N-125753-g6095372a70-20260724",
      path: "C:/Users/test/AppData/Roaming/GuiZhi/tools/ffmpeg.exe",
    });
    renderSection();
    await waitFor(() => expect(row("ffmpeg").getByText("已就绪")).toBeInTheDocument());

    await user.click(row("ffmpeg").getByRole("button", { name: "检查更新" }));

    await waitFor(() =>
      expect(row("ffmpeg").getByText("已是最新")).toBeInTheDocument(),
    );
    expect(window.api.ffmpeg.install).not.toHaveBeenCalled();
  });

  it("本地转写引擎装好后可检查更新，重新安装仍收进高级面板", async () => {
    const user = userEvent.setup();
    renderSection();
    await waitFor(() => expect(row("funasr").getByText("已就绪")).toBeInTheDocument());

    expect(row("funasr").queryByRole("button", { name: "重新安装" })).not.toBeInTheDocument();
    expect(row("funasr").getByRole("button", { name: "检查更新" })).toBeInTheDocument();

    await user.click(row("funasr").getByLabelText("高级选项"));

    expect(
      row("funasr").getByRole("button", { name: "重新安装" }),
    ).toBeInTheDocument();
    expect(
      row("funasr").getByRole("button", { name: "卸载引擎" }),
    ).toBeInTheDocument();
  });

  it("本地转写引擎未安装时仍在主操作位给出一键安装", async () => {
    window.api.funasr.status.mockResolvedValue({
      ...FUNASR_INSTALLED,
      installed: false,
      version: undefined,
    });
    renderSection();

    await waitFor(() =>
      expect(
        row("funasr").getByRole("button", { name: "一键安装" }),
      ).toBeInTheDocument(),
    );
  });

  it("Mac 上 ffmpeg 未安装时给复制 brew 命令，不给一键安装", async () => {
    const user = userEvent.setup({ writeToClipboard: true });
    window.api.ffmpeg.status.mockResolvedValue({
      installed: false,
      source: null,
      managedPath: "/Users/test/Library/Application Support/GuiZhi/tools/ffmpeg",
      installSupported: false,
      installHintCommand: "brew install ffmpeg",
    });
    renderSection();

    await waitFor(() =>
      expect(
        row("ffmpeg").getByRole("button", { name: "复制 brew 命令" }),
      ).toBeInTheDocument(),
    );
    expect(
      row("ffmpeg").queryByRole("button", { name: "一键安装" }),
    ).not.toBeInTheDocument();
    expect(
      row("ffmpeg").getByText(/brew install ffmpeg/),
    ).toBeInTheDocument();

    await user.click(row("ffmpeg").getByRole("button", { name: "复制 brew 命令" }));
    await waitFor(() =>
      expect(screen.getByText(/已复制：brew install ffmpeg/)).toBeInTheDocument(),
    );
  });

  it("Intel Mac 上本地转写不给出安装入口，文案导向 audioText", async () => {
    window.electron.updater.getPlatform.mockResolvedValue("darwin");
    window.api.funasr.status.mockResolvedValue({
      installed: false,
      running: false,
      port: 8620,
      dir: "/Users/test/Library/Application Support/GuiZhi/tools/funasr",
      installSupported: false,
    });
    renderSection();

    await waitFor(() =>
      expect(
        row("funasr").getByText(/本平台未提供本地引擎/),
      ).toBeInTheDocument(),
    );
    expect(
      row("funasr").queryByRole("button", { name: "一键安装" }),
    ).not.toBeInTheDocument();

    await waitFor(() =>
      expect(screen.getByText(/无此能力|此开关无效/)).toBeInTheDocument(),
    );
    expect(
      screen.getByRole("switch", { name: "导入时区分说话人" }),
    ).toBeDisabled();
  });

  it("Apple Silicon Mac 给出 GGUF 一键安装入口，说话人分离仍禁用", async () => {
    window.electron.updater.getPlatform.mockResolvedValue("darwin");
    window.api.funasr.status.mockResolvedValue({
      installed: false,
      running: false,
      port: 8620,
      dir: "/Users/test/Library/Application Support/GuiZhi/tools/funasr",
      installSupported: true,
      installFlavor: "gguf",
    });
    renderSection();

    await waitFor(() =>
      expect(
        row("funasr").getByText(/约需 300MB/),
      ).toBeInTheDocument(),
    );
    expect(
      row("funasr").getByRole("button", { name: "一键安装" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("switch", { name: "导入时区分说话人" }),
    ).toBeDisabled();
  });

  it("IPC 层抛错时给出可读提示，而不是留下未处理的 rejection", async () => {
    const user = userEvent.setup();
    vi.spyOn(console, "error").mockImplementation(() => {});
    window.api.ffmpeg.install.mockRejectedValue(new Error("IPC 通道不可用"));
    renderSection();
    await waitFor(() => expect(row("ffmpeg").getByText("已就绪")).toBeInTheDocument());

    await user.click(row("ffmpeg").getByRole("button", { name: "安装内置版" }));

    await waitFor(() =>
      expect(screen.getByText("安装失败：IPC 通道不可用")).toBeInTheDocument(),
    );
  });

  it("手动重新检测绕过缓存", async () => {
    const user = userEvent.setup();
    renderSection();
    await waitFor(() => expect(row("ytdlp").getByText("已就绪")).toBeInTheDocument());

    await user.click(row("ytdlp").getByLabelText("高级选项"));
    await user.click(row("ytdlp").getByRole("button", { name: "重新检测" }));

    expect(window.api.ytdlp.status).toHaveBeenNthCalledWith(1, false);
    expect(window.api.ytdlp.status).toHaveBeenLastCalledWith(true);
  });
});


describe("本地转写引擎更新", () => {
  it("更新成功但备份保留时展示可展开的警告", async () => {
    window.api.funasr.update.mockResolvedValue({ success: true, version: "1.3.30", warning: "备份清理未完成：tools/funasr/update-backup-ABC123" });
    const user = userEvent.setup();
    renderSection();
    await user.click(await row("funasr").findByRole("button", { name: "检查更新" }));
    await user.click(await row("funasr").findByRole("button", { name: "更新到 v1.3.30" }));
    expect(await screen.findByText("本地转写引擎已更新到 v1.3.30")).toBeInTheDocument();
    await user.click(await screen.findByRole("button", { name: /查看详情/ }));
    expect(await screen.findByText("备份清理未完成：tools/funasr/update-backup-ABC123")).toBeInTheDocument();
  });
  it("先检查，再按展示版本更新，不调用重装", async () => {
    const user = userEvent.setup();
    renderSection();
    await waitFor(() => expect(row("funasr").getByText("已就绪")).toBeInTheDocument());
    await user.click(row("funasr").getByRole("button", { name: "检查更新" }));
    await user.click(await row("funasr").findByRole("button", { name: "更新到 v1.3.30" }));
    await waitFor(() => expect(window.api.funasr.update).toHaveBeenCalledWith("1.3.30"));
    expect(window.api.funasr.install).not.toHaveBeenCalled();
    expect(await screen.findByText("本地转写引擎已更新到 v1.3.30")).toBeInTheDocument();
  });
  it("网络检查失败可重试，不能误报最新", async () => {
    window.api.funasr.checkUpdate.mockRejectedValue(new Error("PyPI offline"));
    const user = userEvent.setup();
    renderSection();
    await user.click(await row("funasr").findByRole("button", { name: "检查更新" }));
    expect(await screen.findByText("检查更新失败，请稍后重试")).toBeInTheDocument();
    expect(row("funasr").queryByText("已是最新")).not.toBeInTheDocument();
    expect(row("funasr").getByRole("button", { name: "检查更新" })).toBeEnabled();
  });
  it("更新失败显示错误并恢复可检查状态", async () => {
    window.api.funasr.update.mockResolvedValue({ success: false, error: "已恢复原版本" });
    const user = userEvent.setup();
    renderSection();
    await user.click(await row("funasr").findByRole("button", { name: "检查更新" }));
    await user.click(await row("funasr").findByRole("button", { name: "更新到 v1.3.30" }));
    expect(await screen.findByText("本地转写引擎更新失败")).toBeInTheDocument();
    expect(await row("funasr").findByRole("button", { name: "检查更新" })).toBeEnabled();
  });
  it("没有新版本时只显示已是最新", async () => {
    window.api.funasr.checkUpdate.mockResolvedValue({ current: "1.3.29", latest: "1.3.29", updateAvailable: false });
    const user = userEvent.setup();
    renderSection();
    await user.click(await row("funasr").findByRole("button", { name: "检查更新" }));
    expect(await row("funasr").findByText("已是最新")).toBeInTheDocument();
    expect(window.api.funasr.update).not.toHaveBeenCalled();
  });
  it("GGUF 明确标注随应用适配更新，不给 pip 更新入口", async () => {
    window.api.funasr.status.mockResolvedValue({ ...FUNASR_INSTALLED, installFlavor: "gguf", updateSupported: false });
    renderSection();
    expect(await row("funasr").findByText("随应用适配更新")).toBeInTheDocument();
    expect(row("funasr").queryByRole("button", { name: "检查更新" })).not.toBeInTheDocument();
  });
});


describe("本地转写引擎更新过程展示", () => {
  it("保留版本、显示阶段，其他维护动作禁用但不转圈", async () => {
    let finish!: (result: { success: boolean; version: string }) => void;
    window.api.funasr.update.mockImplementation(() => new Promise((resolve) => { finish = resolve; }));
    const user = userEvent.setup();
    renderSection();
    await user.click(await row("funasr").findByRole("button", { name: "检查更新" }));
    await user.click(await row("funasr").findByRole("button", { name: "更新到 v1.3.30" }));
    const panel = await row("funasr").findByTestId("funasr-update-progress");
    expect(row("funasr").queryByText("已就绪")).not.toBeInTheDocument();
    expect(row("funasr").getByText(/v1.3.29 → v1.3.30/)).toBeInTheDocument();
    expect(within(panel).getByText("正在确认更新版本")).toBeInTheDocument();
    const progressHandler = window.api.on.mock.calls.find(([channel]) => channel === "funasr:installProgress")[1];
    await act(() => progressHandler({ phase: "prepare", percent: null }));
    expect(within(panel).getByRole("status")).toHaveTextContent("正在检查空间与历史备份");
    expect(within(panel).getByText("准备").closest("li")).toHaveAttribute("aria-current", "step");
    await act(() => progressHandler({ phase: "backup", percent: null }));
    expect(within(panel).getByRole("status")).toHaveTextContent("备份当前引擎");
    expect(within(panel).getByText(/这一步可能需要几分钟/)).toBeInTheDocument();
    expect(within(panel).queryByRole("progressbar")).not.toBeInTheDocument();
    expect(within(panel).getByText("备份").closest("li")).toHaveAttribute("aria-current", "step");
    await user.click(row("funasr").getByLabelText("高级选项"));
    for (const name of ["重新检测", "重新安装", "卸载引擎"]) {
      const button = row("funasr").getByRole("button", { name });
      expect(button).toBeDisabled();
      expect(button.querySelector(".animate-spin")).toBeNull();
    }
    await act(() => progressHandler({ phase: "deps", percent: null, detail: "Collecting funasr==1.3.30" }));
    expect(within(panel).getByRole("status")).toHaveTextContent("正在下载并更新引擎");
    expect(within(panel).getByText("Collecting funasr==1.3.30").closest("details")).not.toHaveAttribute("open");
    expect(screen.queryByText(/约 700MB，需要几分钟/)).not.toBeInTheDocument();
    await act(() => progressHandler({ phase: "rollback", percent: null }));
    expect(within(panel).getByRole("status")).toHaveTextContent("恢复原版本");
    expect(within(panel).queryByRole("list")).not.toBeInTheDocument();
    await act(async () => finish({ success: true, version: "1.3.30" }));
    await waitFor(() => expect(row("funasr").queryByTestId("funasr-update-progress")).not.toBeInTheDocument());
  });
});
