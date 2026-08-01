import {
  Children,
  cloneElement,
  isValidElement,
  type ReactNode,
} from "react";

/**
 * 在纯文本里标出关键字（大小写不敏感），供讨论搜索高亮用。
 */
export function highlightText(text: string, query: string): ReactNode {
  const needle = query.trim();
  if (!needle || !text) {
    return text;
  }
  const lower = text.toLowerCase();
  const needleLower = needle.toLowerCase();
  const nodes: ReactNode[] = [];
  let start = 0;
  let key = 0;
  let idx = lower.indexOf(needleLower);
  while (idx >= 0) {
    if (idx > start) {
      nodes.push(text.slice(start, idx));
    }
    nodes.push(
      <mark
        key={`h-${key++}`}
        className="rounded-sm bg-amber-400/90 px-0.5 text-amber-950"
      >
        {text.slice(idx, idx + needle.length)}
      </mark>,
    );
    start = idx + needle.length;
    idx = lower.indexOf(needleLower, start);
  }
  if (start < text.length) {
    nodes.push(text.slice(start));
  }
  return nodes.length === 1 ? nodes[0] : nodes;
}

/** 递归处理 React 子节点里的字符串，用于 Markdown 组件树 */
export function highlightReactChildren(
  children: ReactNode,
  query: string,
): ReactNode {
  const needle = query.trim();
  if (!needle) {
    return children;
  }
  return Children.map(children, (child) => {
    if (typeof child === "string" || typeof child === "number") {
      return highlightText(String(child), needle);
    }
    if (isValidElement<{ children?: ReactNode }>(child) && child.props.children != null) {
      return cloneElement(child, {
        ...child.props,
        children: highlightReactChildren(child.props.children, needle),
      });
    }
    return child;
  });
}
