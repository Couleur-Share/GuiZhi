// 鸿蒙不跨工程引用 npm 工作区：保留唯一协议源，显式生成副本，由 Linux/Node 测试防止漂移。
import { copyFileSync } from 'node:fs';
const root = new URL('../', import.meta.url);
copyFileSync(new URL('packages/shared/utils/reading-sync-format.ts', root),
  new URL('apps/capture-harmony/entry/src/main/ets/core/ReadingSyncFormat.ts', root));
console.log('已更新鸿蒙阅读同步协议副本');
