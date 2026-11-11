// 生成 Go 守卫移植的 golden：Node 真实的 WHATWG URL 解析、isForbiddenAIEndpointAddress、evaluateEndpointAccess 的输出。
// 用法：pnpm dlx tsx spikes/mygo-ai/make-net-golden.mts <输出.json>
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import Module from "node:module";
process.env.APPDATA = fs.mkdtempSync(path.join(os.tmpdir(), "guizhi-net-golden-"));
const orig = (Module as any)._load;
(Module as any)._load = function (request: string, ...rest: unknown[]) {
  if (request === "electron") return { app: {}, ipcMain: {}, session: {} };
  return orig.call(this, request, ...rest);
};
const { isForbiddenAIEndpointAddress } = await import("../../apps/desktop/src/main/services/net-safety.ts");
const { evaluateEndpointAccess } = await import("../../apps/desktop/src/main/services/ai-endpoint-guard.ts");

const urls = [
  "http://127.0.0.1:11434/v1", "HTTP://LOCALHOST:11434/v1", "http://[::1]:11434", "http://[0:0:0:0:0:0:0:1]/",
  "https://api.openai.com/v1/chat/completions", "https://api.openai.com:443/v1", "https://api.openai.com:8443/v1",
  "http://example.com:80", "http://example.com:81", "http://example.com:00080/", "http://example.com:65535", "http://example.com:65536",
  "http://user:pass@127.0.0.1:80/", "http://127.0.0.1@169.254.169.254/", "http://169.254.169.254/latest/meta-data",
  "http://2852039166/", "http://0xa9fea9fe/", "http://0251.0376.0251.0376/", "http://169.254.43518/", "http://169.254.169.254./",
  "http://127.1/", "http://0x7f.1/", "http://2130706433", "http://0/", "http://0.0.0.0", "http://256.1.1.1/", "http://1.2.3.4.5/",
  "http://1.2.3.256/", "http://4294967296/", "http://4294967295/", "http://foo.0x10/", "http://foo.09/", "http://a.1/", "http://1.a/",
  "http://[::ffff:169.254.169.254]/", "http://[::ffff:a9fe:a9fe]/", "http://[::a9fe:a9fe]/", "http://[::169.254.169.254]/", "http://[::ffff:0:a9fe:a9fe]/",
  "http://[fe80::1]/", "http://[FE80::1]:8080/", "http://[ff02::1]/", "http://[2001:db8::1]/", "http://[2001:0db8:0000:0000:0000:0000:0000:0001]/",
  "http://[1:0:0:2:0:0:0:3]/", "http://[::]/", "http://[::2]/", "http://[::ffff:127.0.0.1]/", "http://[fe80::1%25eth0]/", "http://[::1", "http://[1::2::3]/",
  "http://224.0.0.1/", "http://240.0.0.1/", "http://255.255.255.255/", "http://239.255.255.250/", "http://223.255.255.255/",
  "http://192.168.1.10:1234/v1", "http://10.0.0.5", "http://localhost./", "http://127.evil.com/", "http://xn--fiq228c.com/",
  "ftp://example.com/", "file:///etc/passwd", "javascript:alert(1)", "localhost:11434", "127.0.0.1:11434", "not a url", "", "   ", "//foo.com", "/path",
  "http://", "http:///x", "http:example.com", "http:/example.com", "http:\\\\example.com\\x", " http://example.com/ ", "http://exa\tmple.com/", "http://exa mple.com/",
  "http://exa<mple.com/", "http://exa%41mple.com/", "http://example.com?x=1", "http://example.com#x", "http://EXAMPLE.com:8080/Path?Q=1",
  "mailto:a@b.c", "ws://127.0.0.1/", "data:text/plain,hi",
];
const urlCases = urls.map((input) => {
  try {
    const u = new URL(input);
    return { in: input, ok: true, protocol: u.protocol, host: u.host, hostname: u.hostname };
  } catch { return { in: input, ok: false }; }
});

// 地址：只用 nodeNet.isIP 认的写法（Go 侧由 parseEndpointURL 或 netip 产出同一地址）
const addrs = [
  "127.0.0.1", "10.0.0.1", "169.254.169.254", "169.254.0.1", "169.253.0.1", "224.0.0.1", "239.255.255.255", "223.255.255.255", "240.0.0.1",
  "255.255.255.255", "0.0.0.0", "0.1.2.3", "1.1.1.1", "192.168.0.1", "100.64.0.1", "198.18.0.1",
  "::1", "::", "::2", "::1:0", "::ffff:a9fe:a9fe", "::ffff:169.254.169.254", "::ffff:127.0.0.1", "::ffff:7f00:1", "::ffff:0.0.0.1", "::ffff:e000:1",
  "::a9fe:a9fe", "::a9fe:1", "::7f00:1", "::1:1", "::0.0.0.1", "::ffff:0:a9fe:a9fe", "::ffff:0:7f00:1", "fe80::1", "febf::1", "fec0::1", "fe80::1%eth0",
  "ff02::1", "ff00::", "fd00::1", "2001:db8::1", "2002:a9fe:a9fe::1", "64:ff9b::a9fe:a9fe", "2606:4700:4700::1111", "1::", "0:0:0:0:0:ffff:a9fe:a9fe",
];
const addrCases = addrs.map((a) => ({ addr: a, forbidden: isForbiddenAIEndpointAddress(a) }));

const configured = ["https://api.openai.com", "http://192.168.1.10:1234"];
const gateUrls = [
  "http://127.0.0.1:11434/v1", "http://localhost:1", "http://127.evil.com/", "http://[::1]/", "https://api.openai.com/v1", "https://API.OPENAI.COM/v1",
  "https://api.openai.com:8443/v1", "http://192.168.1.10:1234/v1", "http://192.168.1.10:1235/v1", "https://example.org/", "http://example.org/",
  "http://EXAMPLE.org:80/", "http://example.org:8080/", "not a url", "http://2130706433/", "http://0x7f.1/", "http://[::ffff:7f00:1]/",
];
const gateCases: unknown[] = [];
for (const url of gateUrls) {
  for (const [now, last] of [[100000, 0], [100000, 95000], [100000, 90001], [100000, 90000], [100000, 89999], [5000, 0]]) {
    for (const granted of [[], ["http://example.org", "https://example.org"]]) {
      const r = evaluateEndpointAccess({
        url, now, configuredHosts: new Set(configured), lastUnknownAt: last, grantedHosts: new Set(granted),
      });
      gateCases.push({ url, now, last, granted, configured, result: r });
    }
  }
}
fs.writeFileSync(process.argv[2], JSON.stringify({ urls: urlCases, addrs: addrCases, gate: gateCases }, null, 1));
console.log("urls", urlCases.length, "addrs", addrCases.length, "gate", gateCases.length);
process.exit(0);

