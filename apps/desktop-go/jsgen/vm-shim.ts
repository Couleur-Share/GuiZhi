// goja 没有 node:vm：Script 的构造只用来做语法检查，交给 Go 注入的 __gzCompile（返回 "" 表示通过，否则是错误信息与行号）。
declare const __gzCompile: (code: string, filename: string) => string;
export class Script {
  constructor(code: string, options?: { filename?: string }) {
    const filename = options?.filename ?? "script.js";
    const message = __gzCompile(code, filename);
    if (message) {
      const error = new SyntaxError(message.split("\n")[0]);
      error.stack = `${filename}:${message.split("\n")[1] ?? "1"}\nSyntaxError: ${message.split("\n")[0]}`;
      throw error;
    }
  }
}