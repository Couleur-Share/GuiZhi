/** 原文查找固定脚本：CSS Highlight 不拆改作者 DOM；只接收受限查找消息。 */
export const SNAPSHOT_FIND_BRIDGE = `
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
`;
