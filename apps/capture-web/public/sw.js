const CACHE = "guizhi-capture-v1";
// 离线壳与草稿相互独立；API 与配对请求永不缓存。
self.addEventListener("install", event => event.waitUntil(caches.open(CACHE).then(cache => cache.addAll(["/", "/app-icon.png", "/manifest.webmanifest"]))));
self.addEventListener("activate", event => event.waitUntil(Promise.all([self.clients.claim(), caches.keys().then(keys => Promise.all(keys.filter(k => k !== CACHE).map(k => caches.delete(k))))])));
function draft(input) {
  return new Promise((resolve, reject) => {
    const open = indexedDB.open("guizhi-capture", 1);
    open.onupgradeneeded = () => open.result.createObjectStore("drafts");
    open.onerror = () => reject(open.error);
    open.onsuccess = () => {
      const db = open.result, tx = db.transaction("drafts", "readwrite");
      // 每次分享单独存草稿，不能覆盖尚未发送的上一条。
      tx.objectStore("drafts").put({ requestId: crypto.randomUUID(), input, mode: "auto" }, "share-" + crypto.randomUUID());
      tx.oncomplete = () => { db.close(); resolve(); }; tx.onerror = () => { db.close(); reject(tx.error); };
    };
  });
}
self.addEventListener("fetch", event => {
  const url = new URL(event.request.url);
  if (url.origin !== self.location.origin || url.pathname.startsWith("/v1/") || url.pathname === "/healthz") return;
  if (event.request.method === "POST" && url.pathname === "/share") {
    event.respondWith((async () => {
      const form = await event.request.formData();
      const input = [...new Set([form.get("title"), form.get("text"), form.get("url")].filter(v => typeof v === "string" && v))].join("\n");
      await draft(input); return Response.redirect("/", 303);
    })()); return;
  }
  if (event.request.method !== "GET") return;
  event.respondWith((async () => {
    const cache = await caches.open(CACHE);
    try {
      const response = await fetch(event.request);
      if (response.ok && (url.pathname === "/" || url.pathname.startsWith("/assets/"))) await cache.put(event.request, response.clone());
      return response;
    } catch { return (await cache.match(event.request)) || (event.request.mode === "navigate" ? await cache.match("/") : null) || Response.error(); }
  })());
});
