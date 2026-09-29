// 测试用 hdc 替身：记录每次调用（参数与所用服务端口），并按命令返回固定输出，不接触任何真实设备。
const fs = require('node:fs');
const path = require('node:path');

const log = process.env.HDC_STUB_LOG;
const argv = process.argv.slice(2);
if (log) fs.appendFileSync(log, JSON.stringify({ argv, port: process.env.OHOS_HDC_SERVER_PORT ?? '' }) + '\n');

// 去掉 -t <key> 后按命令分发。
const args = argv[0] === '-t' ? argv.slice(2) : argv;
const joined = args.join(' ');
const layout = {
  attributes: { bounds: '[0,0][1260,2720]', text: '', type: 'root' },
  children: [
    { attributes: { bounds: '[100,200][500,300]', text: '归知收集', type: 'Text', clickable: 'false' }, children: [] },
    { attributes: { bounds: '[100,2500][400,2650]', text: '近期', type: 'Text', clickable: 'true' }, children: [] },
  ],
};

if (joined === 'list targets') {
  if (process.env.HDC_STUB_TARGETS === 'none') console.log('[Empty]');
  else console.log((process.env.HDC_STUB_TARGETS ?? 'TESTDEVICE').split(',').join('\n'));
} else if (args[0] === 'install') {
  if (process.env.HDC_STUB_INSTALL === 'fail') { console.log('[Fail]signature verification failed'); process.exit(1); }
  console.log(`[Info]App install path:${args[args.length - 1]} msg:install bundle successfully.`);
} else if (joined.startsWith('shell aa start')) console.log('start ability successfully.');
else if (joined.startsWith('shell aa force-stop')) console.log('force stop process successfully.');
else if (joined.startsWith('shell snapshot_display')) console.log('snapshot display successfully!');
else if (joined === 'shell uitest dumpLayout') console.log('DumpLayout saved to:/data/local/tmp/layout_1.json');
else if (args[0] === 'file' && args[1] === 'recv') {
  const [remote, local] = [args[2], args[3]];
  fs.mkdirSync(path.dirname(local), { recursive: true });
  fs.writeFileSync(local, remote.endsWith('.json') ? JSON.stringify(layout) : Buffer.from([0xff, 0xd8, 0xff, 0xd9]));
  console.log('FileTransfer finish');
} else if (joined === 'shell hilog -x') {
  console.log('09-29 12:00:00.000 1 2 I A0c0d0/guizhi: send https://example.com/private?token=abc failed');
  console.log('09-29 12:00:01.000 1 2 I A0c0d0/guizhi: Authorization: Bearer secret-value');
  console.log('09-29 12:00:02.000 1 2 I A0c0d0/other: unrelated line');
} else if (args[0] === 'kill') console.log('Kill server finish');
else if (joined.startsWith('shell rm') || joined.startsWith('shell uitest uiInput') || joined === 'shell hilog -r') { /* 成功且无输出 */ }
else { console.error(`未处理的替身命令：${joined}`); process.exit(9); }
