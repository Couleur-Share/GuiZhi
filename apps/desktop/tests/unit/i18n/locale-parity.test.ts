/**
 * 两份 locale 的键必须一一对应。
 *
 * 此前这份对称完全靠人工维护——加一个中文键忘了补英文，
 * 英文界面上就会静默漏出中文，且没有任何环节会报错。
 */
import { describe, expect, it } from "vitest";
import en from "../../../src/renderer/i18n/locales/en.json";
import zh from "../../../src/renderer/i18n/locales/zh.json";

type LocaleTree = { [key: string]: string | LocaleTree };

function flattenKeys(tree: LocaleTree, prefix = ""): string[] {
  return Object.entries(tree).flatMap(([key, value]) =>
    typeof value === "object" && value !== null
      ? flattenKeys(value, `${prefix}${key}.`)
      : [`${prefix}${key}`],
  );
}

const zhKeys = flattenKeys(zh as LocaleTree);
const enKeys = flattenKeys(en as LocaleTree);

describe("i18n locale 键对称", () => {
  it("zh 有的键 en 都有", () => {
    const missing = zhKeys.filter((key) => !new Set(enKeys).has(key));
    expect(missing).toEqual([]);
  });

  it("en 有的键 zh 都有", () => {
    const missing = enKeys.filter((key) => !new Set(zhKeys).has(key));
    expect(missing).toEqual([]);
  });

  it("插值占位符两边一致", () => {
    const placeholders = (value: unknown): string[] =>
      typeof value === "string"
        ? [...value.matchAll(/\{\{(\w+)\}\}/g)].map((m) => m[1]).sort()
        : [];
    const read = (tree: LocaleTree, key: string): unknown =>
      key.split(".").reduce<unknown>(
        (node, part) =>
          node && typeof node === "object"
            ? (node as Record<string, unknown>)[part]
            : undefined,
        tree,
      );

    const mismatched = zhKeys.filter((key) => {
      const zhVars = placeholders(read(zh as LocaleTree, key));
      const enVars = placeholders(read(en as LocaleTree, key));
      return zhVars.join(",") !== enVars.join(",");
    });
    expect(mismatched).toEqual([]);
  });
});
