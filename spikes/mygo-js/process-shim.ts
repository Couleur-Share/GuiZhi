// goja 里的 node:process：与打包横幅里的全局 process 是同一个对象。
export default (globalThis as unknown as { process: unknown }).process;