import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { Modal } from "../../../src/renderer/components/ui/Modal";
import { ContextMenu } from "../../../src/renderer/components/ui/ContextMenu";

describe("通用浮层键盘行为", () => {
  it("弹窗 Tab 在内部首尾循环", () => {
    render(
      <Modal isOpen onClose={vi.fn()} title="测试" showCloseButton={false}>
        <button type="button">第一个</button>
        <button type="button">最后一个</button>
      </Modal>,
    );
    const first = screen.getByRole("button", { name: "第一个" });
    const last = screen.getByRole("button", { name: "最后一个" });
    last.focus();
    fireEvent.keyDown(last, { key: "Tab" });
    expect(document.activeElement).toBe(first);
    first.focus();
    fireEvent.keyDown(first, { key: "Tab", shiftKey: true });
    expect(document.activeElement).toBe(last);
  });

  it("右键菜单暴露 menu/menuitem 语义", () => {
    render(
      <ContextMenu
        x={10}
        y={10}
        onClose={vi.fn()}
        items={[{ label: "编辑", onClick: vi.fn() }]}
      />,
    );
    expect(screen.getByRole("menu")).toBeInTheDocument();
    expect(screen.getByRole("menuitem", { name: "编辑" })).toBeInTheDocument();
  });

  it("右键菜单支持方向键展开和返回子菜单", () => {
    render(
      <ContextMenu
        x={10}
        y={10}
        onClose={vi.fn()}
        items={[
          {
            label: "移动到",
            children: [{ label: "收件箱", onClick: vi.fn() }],
          },
        ]}
      />,
    );
    const parent = screen.getByRole("menuitem", { name: "移动到" });
    parent.focus();
    fireEvent.keyDown(parent, { key: "ArrowRight" });
    const child = screen.getByRole("menuitem", { name: "收件箱" });
    child.focus();
    fireEvent.keyDown(child, { key: "ArrowLeft" });
    expect(document.activeElement).toBe(parent);
  });
});
