import { render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ErrorBoundary } from "../../../src/renderer/components/ui/ErrorBoundary";

function Boom(): never {
  throw new Error("条目类型 forum 不认识");
}

afterEach(() => {
  vi.restoreAllMocks();
});

describe("ErrorBoundary", () => {
  it("没有异常时原样透传子树", () => {
    render(
      <ErrorBoundary>
        <p>知识库</p>
      </ErrorBoundary>,
    );

    expect(screen.getByText("知识库")).toBeTruthy();
  });

  /**
   * 关键行为：子树抛异常时页面上必须留下可读内容。
   * 缺了这一层，React 会卸载整棵树，用户只看到一整块纯色。
   */
  it("子树抛异常时渲染兜底 UI 并带上错误信息", () => {
    vi.spyOn(console, "error").mockImplementation(() => {});

    render(
      <ErrorBoundary>
        <Boom />
      </ErrorBoundary>,
    );

    expect(screen.getByRole("alert")).toBeTruthy();
    expect(screen.getByText(/界面渲染失败/)).toBeTruthy();
    expect(screen.getByText(/条目类型 forum 不认识/)).toBeTruthy();
  });
});
