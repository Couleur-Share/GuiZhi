//go:build windows

package main

// snapshotBridgeJS 是网页快照桥接脚本的原文（jsrc/services/web-capture/snapshot-bridge.ts 的 SNAPSHOT_BRIDGE，逐字拷贝）。
// snapshot 场景用它拼出与引擎同构的快照文档；场景里先核对它的 sha256 等于 reader.SnapshotBridgeSHA256（岛的 CSP 放行的那个哈希）。
// 桥接脚本改了：重新拷贝这一段、更新 reader.SnapshotBridgeSHA256（主包的 TestSnapshotBridgeHashMatchesTheEngineDocument 会先失败）。
const snapshotBridgeJS = `(()=>{const id=document.documentElement.dataset.instance;const send=(type,value)=>parent.postMessage({id,type,value},'*');(()=>{
 const clean=s=>(s||'').replace(/\s+/g,' ').trim();
 const selection=()=>{const s=getSelection();if(!s||s.isCollapsed||!s.rangeCount)return send('article-selection',null);const r=s.getRangeAt(0),n=r.commonAncestorContainer,e=n.nodeType===1?n:n.parentElement;if(!e||e.closest('input,textarea,button,nav,[contenteditable]'))return;const text=s.toString().trim();if(!text||text.length>4000)return send('article-selection',null);const b=r.getBoundingClientRect();send('article-selection',{text,x:b.left,y:b.bottom});};
 document.addEventListener('mouseup',selection);
 document.addEventListener('keyup',e=>{if(e.key==='Shift'||e.key.startsWith('Arrow'))selection();});
 window.addEventListener('message',event=>{const d=event.data;if(event.source!==parent||d?.id!==id||d.type!=='article-locate')return;const v=d.value;if(typeof v?.text!=='string'||v.text.length>4000||typeof v.request!=='string')return;const wanted=clean(v.text);if(!wanted)return;const nodes=[...document.querySelectorAll('p,li,h1,h2,h3,td,blockquote,pre,div')];const target=nodes.reverse().find(n=>clean(n.textContent).includes(wanted));if(target){let p=target;while(p){if(p.tagName==='DETAILS')p.open=true;p=p.parentElement;}target.setAttribute('data-reader-target','');setTimeout(()=>target.removeAttribute('data-reader-target'),2500);}send('article-located',{request:v.request,found:!!target,top:target?target.getBoundingClientRect().top+scrollY:0});});
})();
const findStyle=document.createElement('style');
findStyle.textContent='::highlight(guizhi-find){background:#fde047;color:#171717}::highlight(guizhi-find-active){background:#f59e0b;color:#171717}';
document.head.append(findStyle);
let lastQuery='',matches=[];
window.addEventListener('message',event=>{
  const data=event.data;
  if(event.source!==parent||data?.id!==id||data.type!=='find')return;
  const value=data.value;
  if(!value||typeof value.query!=='string'||value.query.length>1000||!Number.isSafeInteger(value.index)||value.index<0||!Number.isSafeInteger(value.request))return;
  const query=value.query.trim().toLowerCase();
  if(query!==lastQuery){
    lastQuery=query;matches=[];
    if(query){
      const walker=document.createTreeWalker(document.body,NodeFilter.SHOW_TEXT);
      const nodes=[];let text='',node;
      while((node=walker.nextNode())){
        const el=node.parentElement;
        if(!el||el.closest('script,style,noscript')||getComputedStyle(el).visibility==='hidden'||!el.getClientRects().length)continue;
        nodes.push({node,start:text.length,end:text.length+node.length});text+=node.data;
      }
      // 保持 UTF-16 偏移，避免大小写转换改变字符长度后定位越界。
      const folded=text.replace(/[A-Z]/g,c=>c.toLowerCase());
      const needle=value.query.trim().replace(/[A-Z]/g,c=>c.toLowerCase());
      for(let pos=folded.indexOf(needle);pos>=0&&matches.length<10000;pos=folded.indexOf(needle,pos+needle.length)){
        const first=nodes.find(n=>n.end>pos),last=nodes.find(n=>n.end>=pos+needle.length);
        if(!first||!last)continue;
        const range=document.createRange();
        range.setStart(first.node,pos-first.start);range.setEnd(last.node,pos+needle.length-last.start);
        if(range.getClientRects().length)matches.push(range);
      }
    }
    CSS.highlights.set('guizhi-find',new Highlight(...matches));
  }
  const active=matches.length?matches[value.index%matches.length]:null;
  CSS.highlights.set('guizhi-find-active',new Highlight(...(active?[active]:[])));
  send('find-result',{request:value.request,count:matches.length,top:active?active.getBoundingClientRect().top+scrollY:null});
});
document.addEventListener('keydown',event=>{
  if((event.ctrlKey||event.metaKey)&&event.key.toLowerCase()==='f'){
    event.preventDefault();send('find-open',null);
  }
});

let readerTimer;
const readerLayout=()=>{
 clearTimeout(readerTimer);readerTimer=setTimeout(()=>{
  const blocks=[...document.body.querySelectorAll('h1,h2,h3,h4,h5,h6,p,li,strong,b,span')].filter(el=>el.textContent.trim()&&el.getClientRects().length&&(!el.matches('span,strong,b')||(el.textContent.trim().length<=40&&parseInt(getComputedStyle(el).fontWeight)>=600&&getComputedStyle(el).textAlign==='center'))).slice(0,4000);
  const rows=blocks.map((el,index)=>{const rect=el.getBoundingClientRect(),style=getComputedStyle(el),text=el.textContent.trim();
   const emphasized=el.matches('h1,h2,h3,h4,h5,h6')||(text.length<=40&&style.textAlign==='center'&&(parseInt(style.fontWeight)>=600||el.querySelector('strong,b')));
   return {index,top:rect.top+scrollY,height:rect.height,text:text.slice(0,100),heading:!!emphasized&&!blocks.some(other=>other!==el&&el.contains(other)&&other.textContent.trim()===text),font:parseFloat(style.fontSize)};
  });
  const fonts=rows.filter(row=>row.text.length>=40&&row.font>0).map(row=>row.font).sort((a,b)=>a-b);
  send('reader-layout',{rows,font:fonts[Math.floor(fonts.length/2)]||16,ready:[...document.images].every(img=>img.complete)});
 },60);
};
new ResizeObserver(readerLayout).observe(document.body);
document.addEventListener('load',readerLayout,true);document.addEventListener('error',readerLayout,true);readerLayout();
const reportHeight=()=>send('height',Math.ceil(document.documentElement.getBoundingClientRect().height));new ResizeObserver(reportHeight).observe(document.body);window.addEventListener('message',event=>{if(event.source===parent&&event.data?.id===id&&event.data.type==='reader-refresh'){reportHeight();readerLayout();}});document.addEventListener('keydown',e=>{if(e.key==='Escape'&&!e.defaultPrevented)send('escape',null);});document.addEventListener('click',e=>{const a=e.target.closest('a');const img=e.target.closest('img');if(a){e.preventDefault();const href=a.getAttribute('href')||'';if(href.startsWith('#')){const target=document.getElementById(href.slice(1));if(target)send('anchor',target.getBoundingClientRect().top+scrollY);}else send('link',href);}else if(img){e.preventDefault();send('image',img.getAttribute('src'));}});})();`
