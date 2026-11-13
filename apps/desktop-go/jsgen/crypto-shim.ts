// goja 里没有 node:crypto：createHash 转给 Go 注入的 __gzHash（仅支持 sha256 的 hex/base64 摘要，对应 snapshot-sanitize/document 的用法）
declare const __gzHash: (algorithm: string, data: string, encoding: string) => string;
export function createHash(algorithm: string) {
  let data = "";
  const hash = {
    update(chunk: string) {
      data += chunk;
      return hash;
    },
    digest(encoding: string) {
      return __gzHash(algorithm, data, encoding);
    },
  };
  return hash;
}
