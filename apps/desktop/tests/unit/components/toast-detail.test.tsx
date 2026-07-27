import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import {
  ToastProvider,
  useToast,
} from "../../../src/renderer/components/ui/Toast";

/**
 * toast 的「查看详情」是全应用错误可见性的承载点：提示语要短才有人读，
 * 但失败原因不能因此丢掉。这里守住「默认收起、展开能看到原文」这条线。
 */
function Trigger({
  message,
  detail,
}: {
  message: string;
  detail?: string;
}) {
  const { showToast } = useToast();
  return (
    <button
      type="button"
      onClick={() =>
        showToast(message, "error", detail ? { detail } : undefined)
      }
    >
      触发
    </button>
  );
}

function renderToast(props: { message: string; detail?: string }) {
  return render(
    <ToastProvider>
      <Trigger {...props} />
    </ToastProvider>,
  );
}

describe("Toast 的失败原因详情", () => {
  // 测试环境没初始化 i18n，按 aria-expanded 定位折叠开关，不依赖文案
  const detailToggle = (expanded: boolean) =>
    screen.queryByRole("button", { expanded });

  it("详情默认收起，展开后能看到原文", async () => {
    const user = userEvent.setup();
    renderToast({
      message: "已写入 3 张配图，1 张失败",
      detail: "信息井：HTTP 429 rate limit exceeded",
    });

    await user.click(screen.getByRole("button", { name: "触发" }));
    expect(screen.getByText("已写入 3 张配图，1 张失败")).toBeTruthy();
    expect(screen.queryByText(/rate limit exceeded/)).toBeNull();

    await user.click(detailToggle(false)!);
    expect(screen.getByText(/rate limit exceeded/)).toBeTruthy();

    await user.click(detailToggle(true)!);
    expect(screen.queryByText(/rate limit exceeded/)).toBeNull();
  });

  it("没有详情时不出现折叠入口", async () => {
    const user = userEvent.setup();
    renderToast({ message: "保存失败" });

    await user.click(screen.getByRole("button", { name: "触发" }));
    expect(screen.getByText("保存失败")).toBeTruthy();
    expect(detailToggle(false)).toBeNull();
  });
});
