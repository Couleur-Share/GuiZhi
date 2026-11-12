// 探针：渲染进程能否加载 local-image:// 图片（夹具 rv-img1.png 位于 data/assets/images）。
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const load = (src) => new Promise((res) => { const i = new Image(); i.onload = () => res({ ok: true, w: i.naturalWidth, h: i.naturalHeight }); i.onerror = () => res({ ok: false }); i.src = src; setTimeout(() => res({ ok: false, timeout: true }), 5000); });
await sleep(1500);
const out = { origin: location.origin };
out.localImage = await load("local-image://rv-img1.png");
out.localImageEncoded = await load("local-image://" + encodeURIComponent("rv-img1.png"));
out.apiKeys = Object.keys(window.api).filter((k) => /image|media|asset|file/i.test(k));
return out;
