/**
 * 设置 → 正文配图。
 *
 * 与条目里的「编辑风格」弹窗共用 StyleWorkbench，这里守住设置页这条入口
 * 确实装载得起来：列出全部预设、字段可改、保存写得回去。
 */
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import type { IllustrationStyle } from "@guizhi/shared/types";
import { installWindowMocks } from "../../helpers/window";
import { changeLanguage, i18nReady } from "../../../src/renderer/i18n";
import { ToastProvider } from "../../../src/renderer/components/ui/Toast";
import { IllustrationSettings } from "../../../src/renderer/components/settings/IllustrationSettings";

const style = (id: string, name: string): IllustrationStyle => ({
  id,
  name,
  description: `${name}的适用说明`,
  group: "",
  visualDna: "x",
  character: "",
  negative: "",
  aspectRatio: "16:9",
  maxShots: 4,
  maxLabels: 4,
});

const STYLES = [style("hand-note", "手绘笔记"), style("warm-life", "暖调生活")];

let saveStyles: ReturnType<typeof vi.fn>;

beforeAll(async () => {
  await i18nReady;
  await changeLanguage("zh");
});

// 预设是异步取回来的，render 必须留在用例里：放进 beforeEach 的话
// 那次 setState 落在 act 之外，React 会整屏刷 act 警告
function renderSettings() {
  return render(
    <ToastProvider>
      <IllustrationSettings />
    </ToastProvider>,
  );
}

beforeEach(() => {
  saveStyles = vi.fn(async (next: IllustrationStyle[]) => ({
    success: true,
    styles: next,
  }));
  installWindowMocks({
    api: {
      illustration: {
        styles: vi.fn().mockResolvedValue(STYLES),
        saveStyles,
        builtInStyles: vi.fn().mockResolvedValue(STYLES),
        revealStylesFile: vi.fn().mockResolvedValue({ success: true }),
      },
    },
  });
});

describe("设置页的配图风格", () => {
  it("列出全部预设，并带上各自的适用说明", async () => {
    renderSettings();
    await waitFor(() =>
      expect(screen.getByText("暖调生活")).toBeInTheDocument(),
    );
    expect(screen.getByText("手绘笔记的适用说明")).toBeInTheDocument();
  });

  it("改完保存写回预设文件", async () => {
    const user = userEvent.setup();
    renderSettings();
    await waitFor(() =>
      expect(screen.getByLabelText("名称")).toHaveValue("手绘笔记"),
    );

    await user.clear(screen.getByLabelText("名称"));
    await user.type(screen.getByLabelText("名称"), "我的手绘");
    await user.click(screen.getByRole("button", { name: "保存" }));

    await waitFor(() => expect(saveStyles).toHaveBeenCalledTimes(1));
    expect(saveStyles.mock.calls[0][0][0].name).toBe("我的手绘");
  });
});
