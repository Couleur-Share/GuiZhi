import { SNAPSHOT_READER_BRIDGE } from "./snapshot-reader-bridge";
import { SNAPSHOT_FIND_BRIDGE } from "./snapshot-find-bridge";
import { createHash } from "node:crypto";
/** 固定可信桥接脚本；实例标识由应用写在 HTML 根节点。 */
export const SNAPSHOT_BRIDGE = `(()=>{const id=document.documentElement.dataset.instance;const send=(type,value)=>parent.postMessage({id,type,value},'*');${SNAPSHOT_FIND_BRIDGE}${SNAPSHOT_READER_BRIDGE}new ResizeObserver(()=>send('height',Math.ceil(document.documentElement.getBoundingClientRect().height))).observe(document.body);document.addEventListener('keydown',e=>{if(e.key==='Escape'&&!e.defaultPrevented)send('escape',null);});document.addEventListener('click',e=>{const a=e.target.closest('a');const img=e.target.closest('img');if(a){e.preventDefault();const href=a.getAttribute('href')||'';if(href.startsWith('#')){const target=document.getElementById(href.slice(1));if(target)send('anchor',target.getBoundingClientRect().top+scrollY);}else send('link',href);}else if(img){e.preventDefault();send('image',img.getAttribute('src'));}});})();`;
export const SNAPSHOT_BRIDGE_HASH = createHash("sha256")
  .update(SNAPSHOT_BRIDGE)
  .digest("base64");
