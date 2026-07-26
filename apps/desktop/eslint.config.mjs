import js from "@eslint/js";
import tsParser from "@typescript-eslint/parser";
import tsPlugin from "@typescript-eslint/eslint-plugin";
import reactPlugin from "eslint-plugin-react";
import reactHooksPlugin from "eslint-plugin-react-hooks";

const commonGlobals = {
  window: "readonly",
  document: "readonly",
  navigator: "readonly",
  localStorage: "readonly",
  sessionStorage: "readonly",
  fetch: "readonly",
  console: "readonly",
  process: "readonly",
  Buffer: "readonly",
  global: "readonly",
  globalThis: "readonly",
  module: "readonly",
  require: "readonly",
  __dirname: "readonly",
  __filename: "readonly",
  setTimeout: "readonly",
  clearTimeout: "readonly",
  setInterval: "readonly",
  clearInterval: "readonly",
  URL: "readonly",
  TextEncoder: "readonly",
  Blob: "readonly",
  File: "readonly",
  HTMLElement: "readonly",
  HTMLDivElement: "readonly",
  HTMLInputElement: "readonly",
  HTMLTextAreaElement: "readonly",
  KeyboardEvent: "readonly",
  MouseEvent: "readonly",
  Event: "readonly",
};

export default [
  {
    ignores: [
      "dist/**",
      "out/**",
      "node_modules/**",
      "coverage/**",
      "website/**",
      "src/renderer/out/**",
      "**/*.d.ts",
      "**/*.js",
      "*.tsbuildinfo",
    ],
  },
  {
    files: ["**/*.{ts,tsx,mts}"],
    languageOptions: {
      parser: tsParser,
      parserOptions: {
        ecmaVersion: "latest",
        sourceType: "module",
        ecmaFeatures: {
          jsx: true,
        },
      },
      globals: commonGlobals,
    },
    plugins: {
      "@typescript-eslint": tsPlugin,
      react: reactPlugin,
      "react-hooks": reactHooksPlugin,
    },
    settings: {
      react: {
        version: "detect",
      },
    },
    rules: {
      // flat config 不 extend 任何预设时默认零规则，--max-warnings 0 也就形同虚设。
      // 这里显式铺开三层：JS 基础 → TS 推荐 → 类型感知的那几条关键规则。
      ...js.configs.recommended.rules,
      ...tsPlugin.configs["eslint-recommended"].overrides[0].rules,
      ...tsPlugin.configs.recommended.rules,

      "react/react-in-jsx-scope": "off",
      "react/prop-types": "off",
      "react-hooks/rules-of-hooks": "error",
      "react-hooks/exhaustive-deps": "warn",

      "@typescript-eslint/no-unused-vars": [
        "error",
        {
          argsIgnorePattern: "^_",
          varsIgnorePattern: "^_",
          caughtErrors: "none",
        },
      ],
      "@typescript-eslint/no-explicit-any": "off",
      "@typescript-eslint/ban-ts-comment": "off",
      "@typescript-eslint/no-empty-object-type": "off",
      "@typescript-eslint/no-require-imports": "off",
      "no-empty": ["error", { allowEmptyCatch: true }],
      eqeqeq: ["error", "always", { null: "ignore" }],
      // 「先声明、在闭包里读、之后再赋值」是定时器/取消回调的常规写法，
      // 默认设置会把它们判成 const 并给出会编译不过的自动修复
      "prefer-const": ["error", { ignoreReadBeforeAssign: true }],
      // 清洗文件名与路径本来就要按控制字符成段匹配（\u0000-\u001f），
      // 这条规则在本仓库全是误报
      "no-control-regex": "off",
    },
  },
  {
    // 类型感知规则只铺在业务代码与单测上：vite/vitest/playwright 那几个
    // 配置文件不在任何 tsconfig 的 include 里，硬拉进来只会换一批解析错误。
    files: ["src/**/*.{ts,tsx}", "tests/unit/**/*.{ts,tsx}"],
    languageOptions: {
      parser: tsParser,
      parserOptions: {
        project: ["./tsconfig.test.json"],
        tsconfigRootDir: import.meta.dirname,
      },
    },
    plugins: {
      "@typescript-eslint": tsPlugin,
    },
    rules: {
      // 未处理的 Promise 拒绝在这个仓库里只会进 console，界面毫无反应；
      // 有意不等的地方用 void 显式标注
      "@typescript-eslint/no-floating-promises": "error",
      "@typescript-eslint/await-thenable": "error",
      "@typescript-eslint/no-misused-promises": [
        "error",
        { checksVoidReturn: false },
      ],
    },
  },
];
