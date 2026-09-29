const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const { spawnSync } = require('node:child_process');

// device.ps1 是 Windows PowerShell 脚本；这里用 hdc 替身验证命令构造与安全护栏，不接触真实设备。
const windows = process.platform === 'win32';
const script = path.join(__dirname, '../scripts/device.ps1');
const stub = path.join(__dirname, 'fixtures/hdc-stub.cmd');
const options = { skip: windows ? false : '仅 Windows 官方支持的 PowerShell 5.1 环境运行' };

// Windows PowerShell 5.1 在部分杀软环境偶发 AMSI 访问冲突（0xC0000005），与脚本逻辑无关；仅对此退出码重试。
const ACCESS_VIOLATION = [3221225477, -1073741819];

function run(args, env = {}) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'guizhi-device-'));
  const log = path.join(dir, 'hdc.log');
  let result;
  for (let attempt = 0; attempt < 3; attempt++) {
    fs.rmSync(log, { force: true });
    result = spawnSync('powershell', ['-NoProfile', '-ExecutionPolicy', 'Bypass', '-File', script, ...args,
      '-Hdc', stub, '-OutDir', path.join(dir, 'out')], { encoding: 'utf8', env: { ...process.env, HDC_STUB_LOG: log, ...env } });
    if (!ACCESS_VIOLATION.includes(result.status)) break;
  }
  const calls = fs.existsSync(log) ? fs.readFileSync(log, 'utf8').split('\n').filter(Boolean).map(line => JSON.parse(line)) : [];
  const commands = calls.map(call => (call.argv[0] === '-t' ? call.argv.slice(2) : call.argv).join(' '));
  return { status: result.status, stdout: result.stdout, stderr: result.stderr, calls, commands, dir };
}

test('targets 使用独立 hdc 端口 18710，不触碰默认端口', options, () => {
  const result = run(['targets']);
  assert.equal(result.status, 0); assert.match(result.stdout, /TESTDEVICE/);
  assert.deepEqual(result.calls.map(call => call.port), ['18710']);
  assert.equal(run(['targets', '-ServerPort', '19999']).calls[0].port, '19999');
});

test('默认拒绝正式包与非法包名，护栏先于任何 hdc 调用', options, () => {
  const production = run(['launch', '-Bundle', 'com.couleurapp.guizhi.capture']);
  assert.equal(production.status, 2); assert.equal(production.calls.length, 0);
  assert.match(production.stderr, /拒绝操作正式包/);
  for (const bad of ['a;rm -rf', 'ab', 'com.x y.z', '1abc.def']) {
    const result = run(['launch', '-Bundle', bad]); assert.equal(result.status, 2, bad); assert.equal(result.calls.length, 0, bad);
  }
  const forced = run(['launch', '-Bundle', 'com.couleurapp.guizhi.capture', '-Force']);
  assert.equal(forced.status, 0); assert.ok(forced.commands.includes('shell aa start -a EntryAbility -b com.couleurapp.guizhi.capture'));
});

test('launch 默认启动开发包并带上设备标识；stop 强制停止开发包', options, () => {
  const launch = run(['launch']);
  assert.equal(launch.status, 0);
  assert.deepEqual(launch.commands, ['list targets', 'shell aa start -a EntryAbility -b com.couleurapp.guizhi.capture.dev']);
  assert.deepEqual(launch.calls[1].argv.slice(0, 2), ['-t', 'TESTDEVICE']);
  assert.ok(run(['stop']).commands.includes('shell aa force-stop com.couleurapp.guizhi.capture.dev'));
});

test('launch 可指定 Ability（分享诊断页），名称先校验再调用 hdc', options, () => {
  const diag = run(['launch', '-Ability', 'ShareDiagAbility']);
  assert.equal(diag.status, 0);
  assert.deepEqual(diag.commands, ['list targets', 'shell aa start -a ShareDiagAbility -b com.couleurapp.guizhi.capture.dev']);
  for (const bad of ['a;rm -rf', 'A', '1Abc', 'Entry Ability', 'x'.repeat(65)]) {
    const result = run(['launch', '-Ability', bad]);
    assert.equal(result.status, 2, bad); assert.equal(result.calls.length, 0, bad);
    assert.match(result.stderr, /Ability 名称格式不合法/);
  }
});

test('没有设备或有多台设备时明确失败，多台设备可用 -Device 指定', options, () => {
  const none = run(['launch'], { HDC_STUB_TARGETS: 'none' });
  assert.equal(none.status, 3); assert.match(none.stderr, /未发现已授权设备/);
  assert.equal(none.commands.some(command => command.startsWith('shell aa')), false);
  const many = run(['launch'], { HDC_STUB_TARGETS: 'A1,B2' });
  assert.equal(many.status, 3); assert.match(many.stderr, /多台设备/);
  const chosen = run(['launch', '-Device', 'B2'], { HDC_STUB_TARGETS: 'A1,B2' });
  assert.equal(chosen.status, 0); assert.deepEqual(chosen.calls[0].argv.slice(0, 2), ['-t', 'B2']);
});

test('install 拒绝未签名 HAP，成功后报告，hdc 失败则整体失败', options, () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'guizhi-hap-'));
  const unsigned = path.join(dir, 'entry-dev-unsigned.hap'); const signed = path.join(dir, 'entry-dev-signed.hap');
  fs.writeFileSync(unsigned, 'x'); fs.writeFileSync(signed, 'x');
  const refused = run(['install', '-Hap', unsigned]);
  assert.equal(refused.status, 2); assert.equal(refused.commands.some(command => command.startsWith('install')), false);
  const ok = run(['install', '-Hap', signed]);
  assert.equal(ok.status, 0); assert.ok(ok.commands.includes(`install -r ${signed}`));
  const failed = run(['install', '-Hap', signed], { HDC_STUB_INSTALL: 'fail' });
  assert.notEqual(failed.status, 0);
});

test('shot 取回截图并清理设备端临时文件', options, () => {
  const result = run(['shot', '-Name', 'home']);
  assert.equal(result.status, 0);
  assert.deepEqual(result.commands.slice(1), [
    'shell snapshot_display -f /data/local/tmp/guizhi-shot.jpeg',
    `file recv /data/local/tmp/guizhi-shot.jpeg ${result.stdout.match(/产物：(.+)/)[1].trim()}`,
    'shell rm /data/local/tmp/guizhi-shot.jpeg',
  ]);
  const file = result.stdout.match(/产物：(.+)/)[1].trim();
  assert.match(path.basename(file), /^\d{8}-\d{6}-home\.jpeg$/); assert.equal(fs.statSync(file).size, 4);
});

test('dump 保存布局并列出界面文字；tap -Text 按文字中心点击，找不到时不点击', options, () => {
  const dump = run(['dump', '-Name', 'main']);
  assert.equal(dump.status, 0); assert.match(dump.stdout, /归知收集/); assert.match(dump.stdout, /近期/);
  assert.ok(fs.existsSync(dump.stdout.match(/产物：(.+)/)[1].trim()));
  const tap = run(['tap', '-Text', '近期']);
  assert.equal(tap.status, 0); assert.equal(tap.commands.at(-1), 'shell uitest uiInput click 250 2575');
  const missing = run(['tap', '-Text', '不存在的按钮']);
  assert.equal(missing.status, 3); assert.equal(missing.commands.some(command => command.includes('uiInput')), false);
  const point = run(['tap', '-X', '10', '-Y', '20']);
  assert.equal(point.status, 0); assert.equal(point.commands.at(-1), 'shell uitest uiInput click 10 20');
  assert.equal(run(['tap']).status, 2);
  assert.equal(run(['key', '-Key', 'Back']).commands.at(-1), 'shell uitest uiInput keyEvent Back');
  assert.equal(run(['key', '-Key', 'Back;reboot']).status, 2);
});

test('logs 按关键字过滤并脱敏链接与令牌；server-stop 只结束自己端口的服务', options, () => {
  const logs = run(['logs']);
  assert.equal(logs.status, 0);
  assert.match(logs.stdout, /<url>/); assert.match(logs.stdout, /Bearer <redacted>/);
  assert.doesNotMatch(logs.stdout, /example\.com|secret-value|unrelated/);
  const stop = run(['server-stop']);
  assert.deepEqual(stop.calls.map(call => [call.argv.join(' '), call.port]), [['kill', '18710']]);
  assert.ok(run(['logs', '-Clear']).commands.includes('shell hilog -r'));
});
