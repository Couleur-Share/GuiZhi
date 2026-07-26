/**
 * guizhi/no-redundant-title
 *
 * `title` 必须给出元素自己没写出来的信息。`title={x}` 配 `{x}` 只是把同一句话
 * 再念一遍：文字没被截断时纯属噪音，截断了也该靠点开条目、加宽列去看完整内容，
 * 而不是悬停等气泡。判定条件刻意收得很窄——只有「气泡表达式」与「元素渲染出的
 * 某个表达式」源码完全一致才报，改成条件式（`collapsed ? label : undefined`）
 * 就不再命中。
 *
 * 只查小写的原生标签：组件上的 `title` 多半是 `Modal` / `SettingSection` /
 * `ConfirmDialog` 这类自定义 prop，与 DOM 气泡无关。代价是像 `ui/Input` 这种
 * 把 title 透传到 DOM 的组件查不到，那部分仍需人工把关。
 */

const NULLISH_OPERATORS = new Set(["||", "??"]);

/** `x || undefined` / `x ?? undefined` 与 `x` 是同一个气泡，先剥到内核再比 */
function unwrapUndefinedFallback(expression) {
  let current = expression;
  while (
    current.type === "LogicalExpression" &&
    NULLISH_OPERATORS.has(current.operator) &&
    current.right.type === "Identifier" &&
    current.right.name === "undefined"
  ) {
    current = current.left;
  }
  return current;
}

export const noRedundantTitle = {
  meta: {
    type: "problem",
    docs: {
      description: "禁止 title 复读元素自己已经渲染出来的文字",
    },
    schema: [],
    messages: {
      redundant:
        "title 与元素自己渲染的 `{{text}}` 是同一段内容，气泡只会把看得见的字再念一遍。纯图标控件才需要 title；要保留就让它给出元素没显示的信息（绝对时间、完整地址、置灰原因等），或写成条件式。",
    },
  },

  create(context) {
    const sourceCode = context.sourceCode;
    const textOf = (node) =>
      sourceCode.getText(node).replace(/\s+/g, " ").trim();

    /**
     * 收集元素渲染出来的表达式。`{a || b}` 额外记下 a——a 有值时页面上就是 a，
     * 这样 `title={a}` 配 `{a || <占位>}` 也算复读。
     */
    const collectRendered = (children, collected) => {
      for (const child of children) {
        if (child.type === "JSXExpressionContainer") {
          const { expression } = child;
          if (expression.type !== "JSXEmptyExpression") {
            collected.add(textOf(expression));
            if (
              expression.type === "LogicalExpression" &&
              NULLISH_OPERATORS.has(expression.operator)
            ) {
              collected.add(textOf(expression.left));
            }
          }
          continue;
        }
        if (child.type === "JSXElement" || child.type === "JSXFragment") {
          collectRendered(child.children, collected);
        }
      }
      return collected;
    };

    return {
      JSXElement(node) {
        const opening = node.openingElement;
        if (
          opening.name.type !== "JSXIdentifier" ||
          !/^[a-z]/.test(opening.name.name)
        ) {
          return;
        }

        const titleAttribute = opening.attributes.find(
          (attribute) =>
            attribute.type === "JSXAttribute" &&
            attribute.name.type === "JSXIdentifier" &&
            attribute.name.name === "title",
        );
        if (
          !titleAttribute?.value ||
          titleAttribute.value.type !== "JSXExpressionContainer" ||
          titleAttribute.value.expression.type === "JSXEmptyExpression"
        ) {
          return;
        }

        const text = textOf(
          unwrapUndefinedFallback(titleAttribute.value.expression),
        );
        if (collectRendered(node.children, new Set()).has(text)) {
          context.report({
            node: titleAttribute,
            messageId: "redundant",
            data: { text },
          });
        }
      },
    };
  },
};

export default noRedundantTitle;
