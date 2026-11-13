/** 固定可信脚本：只传纯文本、矩形与定位结果，不开放执行任意代码的入口。 */
export const ARTICLE_SELECTION_BRIDGE = `(()=>{
 const clean=s=>(s||'').replace(/\\s+/g,' ').trim();
 const selection=()=>{const s=getSelection();if(!s||s.isCollapsed||!s.rangeCount)return send('article-selection',null);const r=s.getRangeAt(0),n=r.commonAncestorContainer,e=n.nodeType===1?n:n.parentElement;if(!e||e.closest('input,textarea,button,nav,[contenteditable]'))return;const text=s.toString().trim();if(!text||text.length>4000)return send('article-selection',null);const b=r.getBoundingClientRect();send('article-selection',{text,x:b.left,y:b.bottom});};
 document.addEventListener('mouseup',selection);
 document.addEventListener('keyup',e=>{if(e.key==='Shift'||e.key.startsWith('Arrow'))selection();});
 window.addEventListener('message',event=>{const d=event.data;if(event.source!==parent||d?.id!==id||d.type!=='article-locate')return;const v=d.value;if(typeof v?.text!=='string'||v.text.length>4000||typeof v.request!=='string')return;const wanted=clean(v.text);if(!wanted)return;const nodes=[...document.querySelectorAll('p,li,h1,h2,h3,td,blockquote,pre,div')];const target=nodes.reverse().find(n=>clean(n.textContent).includes(wanted));if(target){let p=target;while(p){if(p.tagName==='DETAILS')p.open=true;p=p.parentElement;}target.setAttribute('data-reader-target','');setTimeout(()=>target.removeAttribute('data-reader-target'),2500);}send('article-located',{request:v.request,found:!!target,top:target?target.getBoundingClientRect().top+scrollY:0});});
})();`;
