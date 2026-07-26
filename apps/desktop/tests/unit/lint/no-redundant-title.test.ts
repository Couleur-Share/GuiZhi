import { RuleTester } from "eslint";
import tsParser from "@typescript-eslint/parser";
import { noRedundantTitle } from "../../../eslint-rules/no-redundant-title.mjs";

/**
 * 这条规则挡的是「气泡把元素自己写着的字再念一遍」。
 * 反面用例来自实际改过的那批（会话卡片、标签 chip、表格摘要）；
 * 正面用例盯住不能误伤的三类：纯图标控件、条件式 title、组件自定义 prop。
 */

const ruleTester = new RuleTester({
  languageOptions: {
    parser: tsParser,
    parserOptions: {
      ecmaVersion: "latest",
      sourceType: "module",
      ecmaFeatures: { jsx: true },
    },
  },
});

ruleTester.run("guizhi/no-redundant-title", noRedundantTitle, {
  valid: [
    // 纯图标：气泡是这个按钮唯一的名字
    `const a = <button title={label} aria-label={label}><XIcon /></button>;`,
    `const b = <span title={typeLabel}>{typeMeta.icon}</span>;`,
    // 气泡给的是元素没显示的另一段信息
    `const c = <span title={new Date(at).toLocaleString()}>{formatItemTime(at)}</span>;`,
    `const d = <button title={task.sourceInput}>{task.displayName || task.sourceInput}</button>;`,
    // 条件式：只在文字没露出来的形态下才给气泡
    `const e = <button title={collapsed ? label : undefined}><span>{label}</span></button>;`,
    `const f = <button title={wide ? undefined : label}>{wide ? <span>{label}</span> : null}</button>;`,
    // 大写开头是组件，title 是它自己的 prop，与 DOM 气泡无关
    `const g = <Modal title={pageTitle}>{pageTitle}</Modal>;`,
    // 没有 title 自然不管
    `const h = <span>{tag.name}</span>;`,
  ],
  invalid: [
    {
      code: `const a = <span title={tag.name}>{tag.name}</span>;`,
      errors: [{ messageId: "redundant" }],
    },
    {
      // 嵌套一层也算：卡片外层挂 title，标题写在里层的 span 上
      code: `const b = (
        <button title={session.title}>
          <span className="truncate">{session.title}</span>
          <span>{time}</span>
        </button>
      );`,
      errors: [{ messageId: "redundant" }],
    },
    {
      // title={x || undefined} 与 {x || 占位} 是同一段内容
      code: `const c = <span title={entry.snippet || undefined}>{entry.snippet || <em>-</em>}</span>;`,
      errors: [{ messageId: "redundant" }],
    },
    {
      code: `const d = <button title={t("library.rename", "重命名")}>{t("library.rename", "重命名")}</button>;`,
      errors: [{ messageId: "redundant" }],
    },
  ],
});
