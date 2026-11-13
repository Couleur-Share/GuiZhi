/** 固定阅读行为仅发送数据；没有外链、模型调用和文件能力。 */
export const V3_READER_SCRIPT = `(()=>{
const send=(type,value)=>parent.postMessage({reading:3,type,value},'*');
const root=document.documentElement;root.dataset.theme=matchMedia('(prefers-color-scheme:dark)').matches?'dark':'light';
const layout=()=>send('layout',{headings:[...document.querySelectorAll('h1,h2,h3')].slice(0,200).map((e,i)=>{if(!e.id)e.id='reading-heading-'+i;return{id:e.id,text:e.textContent.slice(0,200),level:Number(e.tagName.slice(1))}}),top:scrollY,height:document.documentElement.scrollHeight});
let timer;addEventListener('scroll',()=>{clearTimeout(timer);timer=setTimeout(layout,150)},{passive:true});
addEventListener('message',e=>{if(e.source!==parent||e.data?.reading!==3)return;const c=e.data.command;if(!c)return;
// 整页按阅读字号缩放，避免模型的 px 字号覆盖宿主设置；保持标题和正文的比例。
if(c.type==='appearance'){root.dataset.theme=c.theme;root.style.setProperty('--reader-font-size','17px');root.style.setProperty('zoom',String(c.fontSize/17));if(c.fontFamily)root.style.setProperty('--reader-font-family',c.fontFamily);layout()}
if(c.type==='scroll')scrollTo(0,c.top);
if(c.type==='heartbeat')send('heartbeat',c.nonce);
if(c.type==='anchor'){let t=document.getElementById(c.id),p=t;while(p){if(p.tagName==='DETAILS')p.open=true;p=p.parentElement}t?.scrollIntoView({block:'start'})}
if(c.type==='find'){document.querySelectorAll('mark[data-reading-find]').forEach(m=>m.replaceWith(document.createTextNode(m.textContent)));document.querySelectorAll('details').forEach(d=>d.open=true);document.body.normalize();const q=String(c.query||'').toLocaleLowerCase();let count=0,targets=[];if(q){const w=document.createTreeWalker(document.body,NodeFilter.SHOW_TEXT,{acceptNode:n=>n.parentElement?.closest('script,style,input,textarea,select')?NodeFilter.FILTER_REJECT:NodeFilter.FILTER_ACCEPT});const nodes=[];while(w.nextNode())nodes.push(w.currentNode);for(const n of nodes){const text=n.textContent,lower=text.toLocaleLowerCase();let pos=0,hit=lower.indexOf(q),f=document.createDocumentFragment();if(hit<0)continue;while(hit>=0&&count<10000){f.append(text.slice(pos,hit));const m=document.createElement('mark');m.dataset.readingFind='';m.textContent=text.slice(hit,hit+q.length);f.append(m);targets.push(m);count++;pos=hit+q.length;hit=lower.indexOf(q,pos)}f.append(text.slice(pos));n.replaceWith(f)}targets[((c.index%count)+count)%count]?.scrollIntoView({block:'center'})}send('find',{count,requestId:c.requestId})}
});
const reportSelection=()=>{const s=getSelection();if(s?.rangeCount&&s.toString().trim()){const b=s.getRangeAt(0).getBoundingClientRect();send('selection',{text:s.toString().trim().slice(0,4000),x:b.left,y:b.bottom})}};document.addEventListener('mouseup',reportSelection);document.addEventListener('keyup',e=>{if(e.shiftKey)reportSelection()});
document.addEventListener('keydown',e=>{if((e.ctrlKey||e.metaKey)&&e.key.toLowerCase()==='f'){e.preventDefault();send('key','find')}if(e.key==='Escape')send('key','escape')});
document.addEventListener('click',e=>{const a=e.target.closest('a');if(a&&!a.getAttribute('href')?.startsWith('#'))e.preventDefault();const img=e.target.closest('img[data-theme-asset]');if(img)send('image',{id:img.dataset.themeAsset})});
addEventListener('reading-script-fault',e=>send('fault',{id:e.detail?.id,message:String(e.detail?.message||'交互初始化失败').slice(0,1000)}));
addEventListener('error',e=>send('fault',{message:String(e.message||'交互运行失败').slice(0,1000)}));addEventListener('unhandledrejection',e=>send('fault',{message:String(e.reason?.message||e.reason||'交互运行失败').slice(0,1000)}));
addEventListener('load',()=>{layout();send('ready',null)});new ResizeObserver(()=>{clearTimeout(timer);timer=setTimeout(layout,150)}).observe(document.body);
})();`;
