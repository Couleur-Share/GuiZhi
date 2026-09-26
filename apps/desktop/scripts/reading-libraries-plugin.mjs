import { buildReadingLibraries } from './reading-libraries-build.mjs';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
export function readingLibrariesPlugin({ buildLibraries = buildReadingLibraries } = {}) {
  let serve = false;
  let resourceRoot;
  let compilation;
  return { name: 'guizhi-reading-libraries',
    configResolved(config) { serve = config.command === 'serve'; },
    resolveId(id) { if (id === 'virtual:reading-libraries') return '\0' + id; }, async load(id) {
    if (id !== '\0virtual:reading-libraries') return;
    // 测试和 Vite SSR 的 serve 模式没有 Rollup 产物，资源写入本实例的临时目录。
    // 生产构建继续使用相邻文件，安装后不依赖开发路径或缓存。
    compilation ??= buildLibraries().catch(error => { compilation = undefined; throw error; });
    const value = await compilation;
    if (!serve) return readingLibraryModule(value, asset => this.emitFile(asset));
    resourceRoot ??= fs.mkdtempSync(path.join(os.tmpdir(), 'guizhi-reading-serve-'));
    return readingLibraryModule(value, asset => {
      const file = path.join(resourceRoot, asset.fileName);
      fs.mkdirSync(path.dirname(file), { recursive: true });
      fs.writeFileSync(file, asset.source);
    }, resourceRoot);
  }, closeBundle() {
    compilation = undefined;
    if (resourceRoot) {
      fs.rmSync(resourceRoot, { recursive: true, force: true });
      resourceRoot = undefined;
    }
  } };
}

// 大型浏览器脚本单独输出；主进程仅在实际生成阅读页时读取，不常驻缓存。
// 不能内联成字符串：入口源码和解码后的字符串会同时保留在 V8 堆中。
export function readingLibraryModule(value, emit, resourceRoot) {
  const getter = (key, source, file) => {
    const fileName = `reading-libraries/${file}.js`;
    emit({ type: 'asset', fileName, source });
    return `get ${key}(){return readLibrary(${JSON.stringify(fileName)})}`;
  };
  const fields = [getter('compiler', value.compiler, 'compiler'), getter('animation', value.animation, 'animation')];
  const runtime = Object.entries(value.runtime).map(([name, source]) => getter(name, source, `runtime-${name}`));
  return `import fs from 'node:fs';import path from 'node:path';
function readLibrary(file){try{return fs.readFileSync(path.join(${resourceRoot ? JSON.stringify(resourceRoot) : '__dirname'},file),'utf8')}catch(error){throw new Error('阅读组件资源读取失败：'+file+'；'+error.message)}}
export default {${fields.join(',')},components:${JSON.stringify(value.components)},runtime:{${runtime.join(',')}}};`;
}
