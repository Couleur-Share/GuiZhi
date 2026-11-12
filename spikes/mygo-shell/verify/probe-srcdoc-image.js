// 探针：沙箱 srcdoc iframe（与网页快照、旧版主题阅读同构：CSP 写 img-src local-image:，图片用 local-image://）
// 里的图片能否加载。iframe 内的脚本把 onload/onerror 结果经 postMessage 回报。
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
await sleep(1500);
const doc = (csp) => `<!doctype html><meta charset="utf-8"><meta http-equiv="Content-Security-Policy" content="${csp}"><body><img id="i" src="local-image://rv-img1.png"><script>
const i=document.getElementById('i');const send=(v)=>parent.postMessage({probe:1,v},'*');
if(i.complete&&i.naturalWidth>0)send({ok:true,w:i.naturalWidth});else{i.onload=()=>send({ok:true,w:i.naturalWidth});i.onerror=()=>send({ok:false})}
setTimeout(()=>send({ok:false,timeout:true}),4000);</script>`;
const run = (csp, viaAttr) => new Promise((resolve) => {
  const f = document.createElement("iframe");
  f.setAttribute("sandbox", "allow-scripts");
  const on = (e) => { if (e.source === f.contentWindow && e.data && e.data.probe) { removeEventListener("message", on); f.remove(); resolve(e.data.v); } };
  addEventListener("message", on);
  document.body.appendChild(f);
  if (viaAttr) f.setAttribute("srcdoc", doc(csp)); else f.srcdoc = doc(csp);
});
const csp = "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src local-image:";
return { property: await run(csp, false), attribute: await run(csp, true) };
