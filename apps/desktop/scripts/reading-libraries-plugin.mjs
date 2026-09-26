import fs from 'node:fs/promises';
import path from 'node:path';
import { createRequire } from 'node:module';
const require = createRequire(import.meta.url);
const root = path.resolve(import.meta.dirname, '..');
const names = ['timeline', 'steps', 'alert', 'badge', 'stat', 'table', 'card'];
let pending;
async function libraries() {
  const { build } = await import('vite');
  const bundle = async name => {
    const result = await build({ configFile: false, root, logLevel: 'error', define: { 'process.env.NODE_ENV': '"production"' }, build: { write: false, minify: true, target: 'chrome130', lib: { entry: path.join(root, `src/reading-graphics/${name}.ts`), name: 'GuiZhiReading', formats: ['iife'] }, rollupOptions: { output: { inlineDynamicImports: true } } } });
    return (Array.isArray(result) ? result[0] : result).output.find(item => item.type === 'chunk').code;
  };
  const components = {};
  const daisy = path.dirname(require.resolve('daisyui/package.json'));
  for (const name of names) {
    // 官方预编译组件保留嵌套规则，不引入 Tailwind 全局样式。
    const value = await fs.readFile(path.join(daisy, `components/${name}.css`), 'utf8');
    components[name] = `:where(.gz-reading-components){${value.replace(/\.([a-zA-Z][\w-]*)/g, '.gz-ui-$1')}}`;
  }
  return { compiler: await bundle('compiler'), animation: await bundle('animation'), components,
    runtime: { echarts: await bundle('runtime-echarts'), mermaid: await bundle('runtime-mermaid'), animation: await bundle('runtime-animation') } };
}
export function readingLibrariesPlugin() {
  return { name: 'guizhi-reading-libraries', resolveId(id) { if (id === 'virtual:reading-libraries') return '\0' + id; }, async load(id) {
    if (id !== '\0virtual:reading-libraries') return;
    pending ??= libraries();
    return readingLibraryModule(await pending, asset => this.emitFile(asset));
  } };
}

// 大型浏览器脚本单独输出；主进程仅在实际生成阅读页时读取，不常驻缓存。
// 不能内联成字符串：入口源码和解码后的字符串会同时保留在 V8 堆中。
export function readingLibraryModule(value, emit) {
  const getter = (key, source, file) => {
    const fileName = `reading-libraries/${file}.js`;
    emit({ type: 'asset', fileName, source });
    return `get ${key}(){return readLibrary(${JSON.stringify(fileName)})}`;
  };
  const fields = [getter('compiler', value.compiler, 'compiler'), getter('animation', value.animation, 'animation')];
  const runtime = Object.entries(value.runtime).map(([name, source]) => getter(name, source, `runtime-${name}`));
  return `import fs from 'node:fs';import path from 'node:path';
function readLibrary(file){try{return fs.readFileSync(path.join(__dirname,file),'utf8')}catch(error){throw new Error('阅读组件资源读取失败：'+file+'；'+error.message)}}
export default {${fields.join(',')},components:${JSON.stringify(value.components)},runtime:{${runtime.join(',')}}};`;
}
