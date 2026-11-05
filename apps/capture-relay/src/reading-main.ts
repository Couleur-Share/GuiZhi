// 独立进程和数据库；不改变现有手机收集中转的入口或默认端口。
import { mkdirSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { createReadingServer } from './reading-sync/server';

const database = resolve(process.env.READING_DATABASE ?? 'data/reading.db');
mkdirSync(dirname(database), { recursive: true, mode: 0o700 });
const { app } = createReadingServer({ database, bootstrapCredential: process.env.READING_BOOTSTRAP_CREDENTIAL ?? '' });
for (const signal of ['SIGINT', 'SIGTERM'] as const) process.once(signal, () => { void app.close(); });
// 默认仅监听本机；生产由明确配置的 HTTPS 反向代理对外提供服务。
await app.listen({ host: '127.0.0.1', port: Number(process.env.READING_PORT ?? 8788) });
const address = app.server.address();
console.log(JSON.stringify({ service: 'guizhi-reading', port: typeof address === 'object' && address ? address.port : null }));
