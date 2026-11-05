// 验证真机诊断报告内的公开夹具密文；不接受真实密钥或读取用户数据库。
import { readFileSync } from 'node:fs';
import { decryptReadingObject } from '../packages/core/src/reading-sync/crypto';

const file = process.argv[2];
if (!file) throw new Error('用法：tsx scripts/verify-reading-report.mts <真机报告.txt>');
const report = readFileSync(file, 'utf8');
if (report.length > 100000) throw new Error('诊断报告超限');
const fixtures = JSON.parse(readFileSync(new URL('../packages/shared/fixtures/reading-sync-v1.json', import.meta.url), 'utf8'));
for (const fixture of fixtures) {
  const prefix = `ROUNDTRIP ${fixture.name} `;
  const rows = report.split(/\r?\n/).filter(line => line.startsWith(prefix));
  if (rows.length !== 1) throw new Error(`诊断报告缺少或重复：${fixture.name}`);
  const plain = decryptReadingObject(Buffer.from(fixture.keyHex, 'hex'), fixture.identity, rows[0].slice(prefix.length));
  if (plain.toString('hex') !== fixture.plainHex) throw new Error(`真机返回内容不匹配：${fixture.name}`);
}
for (const marker of ['PASS asset-store-write-read', 'PASS asset-store-cleanup']) {
  if (!report.split(/\r?\n/).includes(marker)) throw new Error('诊断报告未完成安全存储验证');
}
console.log('报告中 4 个返回密文已由 Node 验证；另需核对报告确实来自本次真机运行，不能用合成报告代替。');
