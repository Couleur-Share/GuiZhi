// goja 没有 Web 的 TextEncoder / TextDecoder：校验器用它们按 UTF-8 字节数核对上限，这里补一份等价实现。
const g = globalThis as Record<string, unknown>;
if (typeof g.TextEncoder === "undefined") {
  g.TextEncoder = class TextEncoder {
    readonly encoding = "utf-8";
    encode(input = ""): Uint8Array {
      const s = String(input), out: number[] = [];
      for (let i = 0; i < s.length; i++) {
        let c = s.charCodeAt(i);
        if (c >= 0xd800 && c <= 0xdbff && i + 1 < s.length) {
          const d = s.charCodeAt(i + 1);
          if (d >= 0xdc00 && d <= 0xdfff) { c = 0x10000 + ((c - 0xd800) << 10) + (d - 0xdc00); i++; }
        }
        if (c >= 0xd800 && c <= 0xdfff) c = 0xfffd; // 孤立代理项：与 WHATWG 一致，替换为 U+FFFD
        if (c < 0x80) out.push(c);
        else if (c < 0x800) out.push(0xc0 | (c >> 6), 0x80 | (c & 63));
        else if (c < 0x10000) out.push(0xe0 | (c >> 12), 0x80 | ((c >> 6) & 63), 0x80 | (c & 63));
        else out.push(0xf0 | (c >> 18), 0x80 | ((c >> 12) & 63), 0x80 | ((c >> 6) & 63), 0x80 | (c & 63));
      }
      return Uint8Array.from(out);
    }
  };
}
export {};

