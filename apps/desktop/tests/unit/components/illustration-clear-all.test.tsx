/**
 * 一键清空正文配图。
 *
 * 图片文件会跟着被回收，删了找不回来，所以必须过二次确认——
 * 而确认框弹出之前，一次 IPC 都不该发出去。
 */
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import type { KnowledgeItem } from "@guizhi/shared/types";
import { installWindowMocks } from "../../helpers/window";
import { changeLanguage, i18nReady } from "../../../src/renderer/i18n";
import { ToastProvider } from "../../../src/renderer/components/ui/Toast";
import { IllustrationPanel } from "../../../src/renderer/components/library/IllustrationPanel";

const item = {
  id: "item-1",
  title: "四象限理性消费",
  content: [
    "第一段正文，长度足够撑起一张配图的信息量，这里再补上一些字。",
    "![高频高影响](local-image://gen-a.png)",
    "第二段正文，同样写得长一点，好让它成为可配图的锚点段落。",
    "![低频低影响](local-image://gen-b.png)",
  ].join("\n\n"),
  collectionId: "col-money",
} as KnowledgeItem;

let clear: ReturnType<typeof vi.fn>;

beforeAll(async () => {
  await i18nReady;
  await changeLanguage("zh");
});

// 风格列表是异步取回来的，render 留在用例里，避免 act 之外的 setState 警告
function renderPanel() {
  return render(
    <ToastProvider>
      <IllustrationPanel item={item} isOpen onClose={() => {}} />
    </ToastProvider>,
  );
}

beforeEach(() => {
  localStorage.clear();
  clear = vi.fn().mockResolvedValue({ success: true, removed: 2 });
  installWindowMocks({
    api: {
      illustration: {
        styles: vi.fn().mockResolvedValue([]),
        onProgress: vi.fn().mockReturnValue(() => {}),
        clear,
      },
    },
  });
});

describe("清空全部配图", () => {
  it("确认之前不发请求，确认之后才清", async () => {
    const user = userEvent.setup();
    renderPanel();
    expect(screen.getByText("正文里的配图（2 张）")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "全部清空" }));
    expect(clear).not.toHaveBeenCalled();

    const dialog = screen.getByRole("alertdialog");
    expect(
      within(dialog).getByText(/2 张配图会被移除/),
    ).toBeInTheDocument();

    await user.click(within(dialog).getByRole("button", { name: "全部清空" }));
    await waitFor(() => expect(clear).toHaveBeenCalledWith("item-1"));
  });

  it("取消就什么也不做", async () => {
    const user = userEvent.setup();
    renderPanel();
    await user.click(screen.getByRole("button", { name: "全部清空" }));
    await user.click(
      within(screen.getByRole("alertdialog")).getByRole("button", {
        name: "取消",
      }),
    );

    expect(clear).not.toHaveBeenCalled();
    expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument();
  });
});
