import { fork } from 'node:child_process';

/** 等待子进程真正退出后才交付产物，确保大编译堆已释放。 */
export function buildReadingLibraries(worker = new URL('./reading-libraries-worker.mjs', import.meta.url), timeoutMs = 600_000) {
  return new Promise((resolve, reject) => {
    const child = fork(worker, [], {
      windowsHide: true,
      execArgv: [],
      stdio: ['ignore', 'ignore', 'pipe', 'ipc'],
    });
    let result;
    let failure;
    let stderr = '';
    let settled = false;
    const timer = setTimeout(() => {
      failure = new Error('阅读组件编译超时，已停止子进程');
      child.kill();
    }, timeoutMs);
    timer.unref();
    child.stderr.on('data', chunk => {
      stderr = (stderr + chunk.toString()).slice(-16_384);
    });
    child.on('message', message => {
      if (message?.ok === true) result = message.value;
      else if (message?.ok === false) failure = new Error(`阅读组件编译失败：${message.error}`);
    });
    const finish = (error) => {
      if (settled) return;
      settled = true;
      clearTimeout(timer);
      if (error) reject(error);
      else resolve(result);
    };
    child.once('error', error => finish(new Error(`阅读组件编译进程启动失败：${error.message}`)));
    child.once('close', (code, signal) => {
      finish(failure || (code === 0 && result ? undefined : new Error(
        `阅读组件编译进程异常退出（${signal || code}）：${stderr || '未返回产物'}`,
      )));
    });
  });
}
