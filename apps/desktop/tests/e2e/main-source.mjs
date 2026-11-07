/**
 * 截图脚本用 tsx 直接加载主进程源码生成夹具。`virtual:reading-libraries` 由构建期 Vite 插件提供，
 * tsx 下不存在，这里映射到只够模块加载的空替身；夹具页面不能依赖真实图形库，需要时改走应用内 IPC。
 */
import Module from "node:module";
import { fileURLToPath } from "node:url";
import { require as tsRequire } from "tsx/cjs/api";

const VIRTUAL_LIBRARIES = "virtual:reading-libraries";
const stubPath = fileURLToPath(new URL("./reading-libraries-stub.cjs", import.meta.url));
const resolveFilename = Module._resolveFilename;
Module._resolveFilename = function (request, ...rest) {
  return request === VIRTUAL_LIBRARIES ? stubPath : resolveFilename.call(this, request, ...rest);
};

export function requireMainSource(relativePath, fromUrl) {
  return tsRequire(relativePath, fromUrl);
}
