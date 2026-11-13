import { createHash } from "node:crypto";
import { SNAPSHOT_BRIDGE } from "../web-capture/snapshot-bridge";

/** 固定代码复用阅读器消息契约；只新增外观同步和查找前展开折叠区。 */
const appearance = `(()=>{
 const id=document.documentElement.dataset.instance;
 document.addEventListener('click',event=>{
  const link=event.target.closest('a[data-reader-toc-link]');if(!link)return;
  const target=document.getElementById((link.getAttribute('href')||'').slice(1));if(!target)return;
  for(const element of document.querySelectorAll('[data-reader-target]'))element.removeAttribute('data-reader-target');
  for(const element of document.querySelectorAll('[data-reader-toc-link]'))element.removeAttribute('aria-current');
  link.setAttribute('aria-current','location');target.setAttribute('data-reader-target','');
  // 与标准阅读器共享锚点消息前先展开目标祖先，折叠章节也能准确定位。
  let parent=target;while(parent){if(parent.tagName==='DETAILS')parent.open=true;parent=parent.parentElement;}
 },true);
 window.addEventListener('message',event=>{
  const data=event.data;if(event.source!==parent||data?.id!==id)return;
  if(data.type==='appearance'){
   const value=data.value;if(!value||!['light','dark'].includes(value.theme))return;
   document.documentElement.dataset.theme=value.theme;
   if(typeof value.fontSize==='number'&&Number.isFinite(value.fontSize)&&value.fontSize>=12&&value.fontSize<=32)document.documentElement.style.setProperty('--reader-font-size',value.fontSize+'px');
   if(typeof value.fontFamily==='string'&&value.fontFamily.length<=100&&/^[a-zA-Z0-9\\s,'"\\u4e00-\\u9fff-]+$/.test(value.fontFamily))document.documentElement.style.setProperty('--reader-font-family',value.fontFamily);
  }
  if(data.type==='find'&&typeof data.value?.query==='string'&&data.value.query.length<=1000&&data.value.query.trim())for(const element of document.querySelectorAll('details'))element.open=true;
 },true);
})();`;
export const THEMED_READING_BRIDGE = appearance + SNAPSHOT_BRIDGE;
export const THEMED_READING_BRIDGE_HASH = createHash("sha256").update(THEMED_READING_BRIDGE).digest("base64");
