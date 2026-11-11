// css-tree 的精简入口：snapshot-sanitize.ts 只用 parse / walk / generate。
// 完整入口会带上 mdn-data 的 JSON（语法校验器），在 goja 里既用不到也加载不了；
// 子路径入口的默认导出就是各自的函数（与 css-tree/lib/index.js 里导出的同一实现）。
import parseImpl from "css-tree/parser";
import generateImpl from "css-tree/generator";
import walkImpl from "css-tree/walker";

export const parse = parseImpl;
export const generate = generateImpl;
export const walk = walkImpl;
