// 出站网络层纯逻辑的差分基准：net-safety.ts（SSRF 地址判定）与 shared/utils/network-proxy.ts（代理设置归一化）。
// DNS 解析不进基准（依赖真实网络），主机名路径由 Go 侧用注入的 resolver 单测。
export async function buildNetGolden(): Promise<unknown> {
  const ns = await import("../../apps/desktop/src/main/services/net-safety.ts");
  const np = await import("../../packages/shared/utils/network-proxy.ts");

  const ips = [
    "0.0.0.0", "0.1.2.3", "1.1.1.1", "8.8.8.8", "9.255.255.255", "10.0.0.1", "10.255.255.255", "11.0.0.1",
    "100.63.255.255", "100.64.0.1", "100.127.255.255", "100.128.0.1", "127.0.0.1", "127.255.255.255", "128.0.0.1",
    "169.253.255.255", "169.254.169.254", "169.255.0.1", "172.15.255.255", "172.16.0.1", "172.31.255.255", "172.32.0.1",
    "192.0.0.1", "192.0.2.1", "192.0.3.1", "192.167.1.1", "192.168.0.1", "192.169.0.1", "198.17.0.1", "198.18.0.1",
    "198.19.255.255", "198.20.0.1", "198.51.100.7", "198.51.101.7", "203.0.113.9", "203.0.114.9", "223.255.255.255",
    "224.0.0.1", "239.255.255.255", "240.0.0.1", "255.255.255.255", "010.0.0.1", "1.2.3", "256.1.1.1", "1.2.3.4.5",
    "::", "::1", "::2", "::ffff:127.0.0.1", "::ffff:7f00:1", "::ffff:8.8.8.8", "::ffff:808:808", "::ffff:198.18.0.1",
    "::ffff:c612:1", "::ffff:0:7f00:1", "::ffff:0:808:808", "::ffff:0:c612:1", "::7f00:1", "::808:808", "::a9fe:a9fe",
    "::1:2", "0:0:0:0:0:0:7f00:1", "fc00::1", "fd12:3456::1", "fe00::1", "fe80::1", "fe80::1%eth0", "febf::1", "fec0::1",
    "ff00::1", "ff02::1", "2001:db8::1", "2001:0:4136:e378:8000:63bf:3fff:fdd2", "2001:4860:4860::8888", "2002:c000:204::1",
    "2606:4700:4700::1111", "2a00:1450:4001::200e", "100::1", "0100::1", "64:ff9b::808:808", "64:ff9b::7f00:1",
    "2001:1::1", "2000::1", "3fff::1", "::ffff:0.0.0.0", "FE80::1", "FC00::ABCD", "example.com", "", "localhost",
  ];
  const addr = ips.map((a) => ({
    a,
    priv: ns.isPrivateAddress(a),
    forbidden: ns.isForbiddenAIEndpointAddress(a),
  }));

  const hostnames = [
    "localhost", "LOCALHOST", "a.localhost", "localhost.localdomain", "x.localdomain", "localdomain", "example.com",
    "notlocalhost", "localhost.example.com", "foo.localhost.", "",
  ];
  const blocked = hostnames.map((h) => ({ h, blocked: ns.isBlockedHostname(h) }));

  const optionSets = [
    {},
    { allowPrivateNetwork: true },
    { allowProxyCompatibilityAddress: true },
    { allowPrivateNetwork: true, allowProxyCompatibilityAddress: true },
  ];
  const resolveHosts = ["localhost", "a.localhost", "1.1.1.1", "10.0.0.1", "198.18.0.1", "198.19.1.1", "::1", "fe80::1", "::ffff:198.18.0.1", "::ffff:c612:1", "8.8.8.8", "2606:4700:4700::1111"];
  const resolve: unknown[] = [];
  for (const host of resolveHosts) {
    for (const [i, opts] of optionSets.entries()) {
      try {
        const r = await ns.resolvePublicAddress(host, opts);
        resolve.push({ host, opt: i, ok: true, address: r.address, family: r.family });
      } catch (e) {
        resolve.push({ host, opt: i, ok: false, error: (e as Error).message });
      }
    }
  }

  const proxyInputs: unknown[] = [
    undefined, null, 5, "x", [], {}, { mode: "manual" }, { mode: "bogus" }, { mode: "direct" },
    { mode: "manual", host: "127.0.0.1", port: 7890 },
    { mode: "manual", host: "127.0.0.1", port: "8080" },
    { mode: "manual", host: "127.0.0.1", port: "8080abc" },
    { mode: "manual", host: "127.0.0.1", port: "  9090" },
    { mode: "manual", host: "127.0.0.1", port: 0 },
    { mode: "manual", host: "127.0.0.1", port: 65536 },
    { mode: "manual", host: "127.0.0.1", port: 65535 },
    { mode: "manual", host: "127.0.0.1", port: 1.5 },
    { mode: "manual", host: "127.0.0.1", port: -1 },
    { mode: "manual", host: "127.0.0.1", port: "+80" },
    { mode: "manual", host: "127.0.0.1", port: null },
    { mode: "manual", host: "http://proxy.local:3128" },
    { mode: "manual", host: "http://proxy.local" },
    { mode: "manual", host: "http://proxy.local:80" },
    { mode: "manual", host: "https://proxy.local:443", port: 1234 },
    { mode: "manual", host: "https://proxy.local:8443" },
    { mode: "manual", host: "socks5://u:p@10.0.0.2:1080" },
    { mode: "manual", host: "socks5://us%40er:p%3Aw@10.0.0.2:1080", username: "x", password: "y" },
    { mode: "manual", host: "socks5://10.0.0.2", protocol: "http", port: 1111, username: "keep" },
    { mode: "manual", host: "ftp://p.local:21" },
    { mode: "manual", host: "http://[::1]:7890" },
    { mode: "manual", host: "::1" },
    { mode: "manual", host: "[::1]" },
    { mode: "manual", host: "HTTP://UPPER.Local:99" },
    { mode: "manual", host: "proxy.local\r\n", username: "a\tb", password: "p\x00w" },
    { mode: "manual", host: "  spaced  " },
    { mode: "manual", host: "x".repeat(600) },
    { mode: "manual", host: "h", password: "q".repeat(1100) },
    { mode: "manual", host: "h", username: "é中文 !*'()~" , password: "a b/c?d" },
    { mode: "manual", protocol: "socks5", host: "h", port: 1080 },
    { mode: "manual", protocol: "ftp", host: "h" },
    { mode: "manual", host: 5 },
    { mode: "manual", host: "http://", port: 1 },
    { mode: "manual", host: "://x" },
    { mode: "system", bypass: "" },
    { mode: "system", bypass: "a.com; b.com\n*.c.com,,  .d.com ,<local>" },
    { mode: "system", bypass: "x\x01y,\tz" },
    { mode: "system", bypass: "q".repeat(2100) },
    { mode: "system", bypass: 5 },
    { mode: "system", bypass: ",,;;" },
  ];
  const normalize = proxyInputs.map((input) => {
    const settings = np.normalizeNetworkProxySettings(input);
    return { input: input === undefined ? "__undefined__" : input, settings, url: np.buildProxyUrl(settings) };
  });

  const bypassRules = [
    "<local>,localhost,127.0.0.1,::1", "*.example.com", ".example.com", "example.com", "<local>", "", "A.COM, B.com",
    "*.a.b.c", "localhost;127.0.0.1", "*example.com", "foo.*",
  ];
  const bypassHosts = [
    "localhost", "LOCALHOST", "example.com", "www.example.com", "example.com.cn", "a.b.c", "x.a.b.c", "intranet", "my.server",
    "127.0.0.1", "::1", "[::1]", "", "  ", " example.com ", "b.com", "xexample.com", "a.com",
  ];
  const bypass: unknown[] = [];
  for (const rules of bypassRules) for (const host of bypassHosts) {
    bypass.push({ rules, host, r: np.shouldBypassProxyForHostname(host, rules) });
  }

  return { addr, blocked, resolve, normalize, bypass };
}
