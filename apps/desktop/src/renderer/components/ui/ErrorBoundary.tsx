import { Component } from "react";
import type { ErrorInfo, ReactNode } from "react";

interface ErrorBoundaryProps {
  children: ReactNode;
}

interface ErrorBoundaryState {
  error: Error | null;
  componentStack: string | null;
}

/**
 * 根级渲染兜底。
 *
 * 没有这一层时，任何一次渲染异常都会让 React 卸载整棵树，页面上只剩
 * index.html 的防白闪底色：用户看到一整块纯色，开发者也拿不到任何线索。
 * v0.6.0 读到新版本写入的未知条目类型时就是这样，最后只能靠远程调试
 * 才定位到抛异常的组件。
 *
 * 文案刻意不走 i18n——i18n 自身出问题时，这一层仍然必须显示得出来。
 */
export class ErrorBoundary extends Component<
  ErrorBoundaryProps,
  ErrorBoundaryState
> {
  state: ErrorBoundaryState = { error: null, componentStack: null };

  static getDerivedStateFromError(
    error: Error,
  ): Pick<ErrorBoundaryState, "error"> {
    return { error };
  }

  componentDidCatch(error: Error, info: ErrorInfo): void {
    console.error("界面渲染失败：", error, info.componentStack);
    this.setState({ componentStack: info.componentStack ?? null });
  }

  private handleReload = (): void => {
    window.location.reload();
  };

  render(): ReactNode {
    const { error, componentStack } = this.state;
    if (!error) {
      return this.props.children;
    }

    const detail = [
      error.stack ?? `${error.name}: ${error.message}`,
      componentStack,
    ]
      .filter(Boolean)
      .join("\n");

    return (
      <div
        role="alert"
        className="flex h-screen w-screen items-center justify-center bg-background p-8 text-foreground"
      >
        <div className="w-full max-w-2xl space-y-3">
          <h1 className="text-lg font-semibold">
            界面渲染失败 / Something went wrong
          </h1>
          <p className="text-sm text-muted-foreground">
            数据没有丢失，只是这次渲染出了错。重新加载通常就能恢复；如果每次打开都这样，请连同下面的错误信息一起反馈。
          </p>
          <pre className="max-h-80 overflow-auto whitespace-pre-wrap rounded-lg border border-border bg-muted/50 p-3 text-xs leading-relaxed">
            {detail}
          </pre>
          <button
            type="button"
            onClick={this.handleReload}
            className="rounded-lg bg-primary px-4 py-2 text-sm font-medium text-primary-foreground transition-opacity hover:opacity-90"
          >
            重新加载 / Reload
          </button>
        </div>
      </div>
    );
  }
}
