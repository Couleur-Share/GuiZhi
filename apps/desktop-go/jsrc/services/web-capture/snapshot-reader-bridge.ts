/** 阅读增强固定脚本：只传递正文结构与数值，不执行宿主提供的代码。 */
export const SNAPSHOT_READER_BRIDGE = `
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
`;
