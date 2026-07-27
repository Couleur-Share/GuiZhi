/**
 * 配图风格编辑器。
 *
 * 这个入口此前是把 config/illustration-styles.json 交给系统默认程序打开，
 * Windows 上就是一个「选择一个应用」框。改成应用内表单之后，守住三条：
 * 字段改得动、必填项空着不会静默存成一份丢失的风格、保存结果回灌得回面板。
 */
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import type { IllustrationStyle } from "@guizhi/shared/types";
import { installWindowMocks } from "../../helpers/window";
import { changeLanguage, i18nReady } from "../../../src/renderer/i18n";
import { ToastProvider } from "../../../src/renderer/components/ui/Toast";
import { StyleEditorModal } from "../../../src/renderer/components/illustration/StyleEditorModal";

const style = (overrides: Partial<IllustrationStyle> = {}): IllustrationStyle => ({
  id: "hand-note",
  name: "手绘笔记",
  description: "白底手绘线稿",
  group: "",
  visualDna: "Pure white background, hand-drawn line art.",
  character: "",
  negative: "No gradients.",
  aspectRatio: "16:9",
  maxShots: 5,
  maxLabels: 5,
  ...overrides,
});

let saveStyles: ReturnType<typeof vi.fn>;
let onSaved: ReturnType<typeof vi.fn>;
let onClose: ReturnType<typeof vi.fn>;

function renderEditor(styles: IllustrationStyle[]) {
  return render(
    <ToastProvider>
      <StyleEditorModal
        isOpen
        styles={styles}
        onClose={onClose}
        onSaved={onSaved}
      />
    </ToastProvider>,
  );
}

beforeAll(async () => {
  await i18nReady;
  await changeLanguage("zh");
});

beforeEach(() => {
  saveStyles = vi.fn(async (next: IllustrationStyle[]) => ({
    success: true,
    styles: next,
  }));
  onSaved = vi.fn();
  onClose = vi.fn();
  installWindowMocks({
    api: {
      illustration: {
        saveStyles,
        builtInStyles: vi.fn().mockResolvedValue([style()]),
        revealStylesFile: vi.fn().mockResolvedValue({ success: true }),
      },
    },
  });
});

describe("IllustrationStyleEditor", () => {
  it("改完字段保存，落盘结果回灌给面板", async () => {
    const user = userEvent.setup();
    renderEditor([style()]);

    await user.clear(screen.getByLabelText("名称"));
    await user.type(screen.getByLabelText("名称"), "我的风格");
    await user.click(screen.getByRole("button", { name: "保存" }));

    await waitFor(() => expect(saveStyles).toHaveBeenCalledTimes(1));
    expect(saveStyles.mock.calls[0][0][0].name).toBe("我的风格");
    expect(onSaved).toHaveBeenCalledWith([
      expect.objectContaining({ name: "我的风格" }),
    ]);
    expect(onClose).toHaveBeenCalled();
  });

  // 画法为空的风格会在读取时被静默丢掉，保存前必须拦住并说清是哪一条
  it("画法清空后不发保存请求，就地给出原因", async () => {
    const user = userEvent.setup();
    renderEditor([style()]);

    await user.clear(screen.getByLabelText("画法与配色"));
    await user.click(screen.getByRole("button", { name: "保存" }));

    expect(saveStyles).not.toHaveBeenCalled();
    expect(
      screen.getByText("画法与配色不能为空，它是生图提示词的主体"),
    ).toBeInTheDocument();
    expect(onClose).not.toHaveBeenCalled();
  });

  it("只剩一套时不让删，且说得出为什么", () => {
    renderEditor([style()]);

    const remove = screen.getByRole("button", { name: "删除" });
    expect(remove).toBeDisabled();
    expect(remove).toHaveAttribute("title", "至少要保留一套风格");
  });

  it("新建的风格进入列表并成为当前编辑对象", async () => {
    const user = userEvent.setup();
    renderEditor([style()]);

    await user.click(screen.getByRole("button", { name: "新建风格" }));

    expect(screen.getByLabelText("名称")).toHaveValue("新风格");
    expect(screen.getByRole("button", { name: "删除" })).toBeEnabled();
  });
});
