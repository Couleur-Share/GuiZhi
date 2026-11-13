// goja 没有全局 URL：校验器用它判断资料地址是否公开可用、搜索代码用它去掉 hash。解析交给 Go 的 WHATWG 实现（__gzURLParse），
// 这里只做属性读写；href 由各组成部分重新拼出，与 WHATWG 序列化一致（特殊协议带 //、无端口时省略）。
declare const __gzURLParse: (input: string, base: string, hasBase: boolean) => string;
const g = globalThis as Record<string, unknown>;
if (typeof g.URL === "undefined") {
  class GzURL {
    protocol = ""; username = ""; password = ""; hostname = ""; port = ""; pathname = ""; search = ""; _hash = ""; opaque = false;
    constructor(input: string, base?: string) {
      const raw = __gzURLParse(String(input), base === undefined ? "" : String(base), base !== undefined);
      if (!raw) { const e = new TypeError("Invalid URL") as TypeError & { code: string; input: string }; e.code = "ERR_INVALID_URL"; e.input = String(input); throw e; }
      Object.assign(this, JSON.parse(raw));
    }
    get hash() { return this._hash; }
    set hash(v: string) { const s = String(v); this._hash = s === "" || s === "#" ? "" : s.startsWith("#") ? s : "#" + s; }
    get host() { return this.hostname + (this.port ? ":" + this.port : ""); }
    get origin() { return /^(https?|ftp|wss?):$/.test(this.protocol) ? `${this.protocol}//${this.host}` : "null"; }
    get href() {
      const auth = this.username || this.password ? this.username + (this.password ? ":" + this.password : "") + "@" : "";
      const special = /^(https?|ftp|wss?|file):$/.test(this.protocol);
      return this.protocol + (special || this.hostname ? "//" + auth + this.host : "") + this.pathname + this.search + this._hash;
    }
    toString() { return this.href; }
    toJSON() { return this.href; }
  }
  g.URL = GzURL;
}
export {};

