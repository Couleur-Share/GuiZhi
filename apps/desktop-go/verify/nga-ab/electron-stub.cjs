// 探针用 electron 桩：产品里 network-proxy / media-files 等只在函数内部用 electron，这里让任意具名导入都能解析。
// 真被调用会得到 undefined，不会悄悄成功。
const handler = { get: (_t, k) => (k === "__esModule" ? false : new Proxy(function () {}, { get: () => undefined, apply: () => undefined })) };
module.exports = new Proxy({}, handler);
