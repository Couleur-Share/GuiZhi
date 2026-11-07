import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { test } from "node:test";
import { analyzeI18n } from "../check-i18n.mjs";

const VIEW = "apps/desktop/src/renderer/components/View.tsx";

function fixture() {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "guizhi-i18n-"));
  const write = (file, content) => {
    fs.mkdirSync(path.dirname(path.join(root, file)), { recursive: true });
    fs.writeFileSync(path.join(root, file), typeof content === "string" ? content : JSON.stringify(content));
  };
  write("apps/desktop/src/renderer/i18n/locales/zh.json", {
    a: { used: "已用", prop: "属性", unused: "无用", dyn: { x: "动态" }, concat: { y: "拼接" } },
  });
  write("apps/desktop/src/renderer/i18n/locales/en.json", {
    a: { used: "Used", prop: "漏译", unused: "Unused", dyn: { x: "Dynamic" }, concat: { y: "Concat" } },
  });
  write(VIEW, `import { Trans, useTranslation } from "react-i18next";
const meta = { labelKey: "a.prop", fallback: "属性兜底", titleFb: "标题兜底" };
export const storeLabel = () => translate("a.viaTranslate", "非组件兜底");
export function View({ kind, part }: { kind: string; part: string }) {
  const { t } = useTranslation();
  console.warn("日志可以是中文");
  if (!kind) throw new Error("诊断信息可以是中文");
  return (
    <div title="写死的标题" data-meta={meta.labelKey}>
      {t("a.used", "已用")}
      {t(\`a.dyn.\${kind}\`)}
      {t("a.concat." + part)}
      {t("a.missing", "缺失")}
      <Trans i18nKey="a.used">子节点中文</Trans>
      写死的正文
    </div>
  );
}
`);
  write("apps/desktop/src/renderer/services/prompt.ts", `export const prompt = "提示词不计入";\n`);
  return root;
}

test("报告缺失键、无人引用的键、英文漏译与写死中文，放行 t/translate 兜底、日志与提示词", () => {
  const root = fixture();
  try {
    const { errors, counts } = analyzeI18n({ root, baseline: {} });
    assert.deepEqual(counts, { [VIEW]: 2 });
    assert.ok(errors.some((e) => e.includes("不存在的键 a.missing")));
    assert.ok(errors.some((e) => e.includes("不存在的键 a.viaTranslate")));
    assert.ok(errors.some((e) => e.includes("en.json 的 a.prop 含中文")));
    assert.deepEqual(errors.filter((e) => e.includes("未被引用")), ["语言包中的键未被引用：a.unused"]);
    assert.ok(errors.some((e) => e.startsWith(`${VIEW} 写死的中文 2 处，基线允许 0 处`)));
  } finally {
    fs.rmSync(root, { recursive: true, force: true });
  }
});

test("基线只减不增：等于基线通过，低于基线要求收紧", () => {
  const root = fixture();
  try {
    const hardcodedErrors = (baseline) =>
      analyzeI18n({ root, baseline }).errors.filter((e) => e.startsWith(VIEW));
    assert.deepEqual(hardcodedErrors({ [VIEW]: 2 }), []);
    assert.deepEqual(hardcodedErrors({ [VIEW]: 3 }), [`${VIEW} 写死的中文已降到 2 处，请把基线改为 2`]);
    assert.deepEqual(hardcodedErrors(null), []);
  } finally {
    fs.rmSync(root, { recursive: true, force: true });
  }
});
