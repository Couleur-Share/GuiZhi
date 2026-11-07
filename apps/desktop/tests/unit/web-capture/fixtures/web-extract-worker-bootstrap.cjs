// 测试用 worker 引导：用 tsx 加载提取线程源码（安装包里加载的是打包后的单文件）。
// 传入 probePort 时，在同一线程里逐一尝试联网，回报每种方式是否被拦下。
const { workerData, parentPort } = require("node:worker_threads");
const { require: tsRequire } = require("tsx/cjs/api");

tsRequire(workerData.entry, __filename);

if (workerData.probePort) {
  const port = workerData.probePort;
  const target = `http://127.0.0.1:${port}/probe`;
  const attempts = {
    fetch: () => fetch(target),
    netConnect: () => require("node:net").connect(port, "127.0.0.1"),
    socketConnect: () => new (require("node:net").Socket)().connect(port, "127.0.0.1"),
    httpGet: () => require("node:http").get(target),
    httpsRequest: () => require("node:https").request(`https://127.0.0.1:${port}/probe`),
    tlsConnect: () => require("node:tls").connect(port, "127.0.0.1"),
    dnsLookup: () => require("node:dns").lookup("localhost", () => undefined),
    dnsPromises: () => require("node:dns").promises.lookup("localhost"),
    udpSocket: () => require("node:dgram").createSocket("udp4"),
  };
  const results = {};
  for (const [name, attempt] of Object.entries(attempts)) {
    try {
      const value = attempt();
      results[name] = "allowed";
      if (value && typeof value.then === "function") value.catch(() => undefined);
      if (value && typeof value.destroy === "function") value.destroy();
      if (value && typeof value.close === "function") value.close();
    } catch (error) {
      results[name] = error instanceof Error ? error.message : String(error);
    }
  }
  parentPort.postMessage({ probe: results });
}
