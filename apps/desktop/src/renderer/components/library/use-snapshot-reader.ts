import { useEffect, useRef, useState, type RefObject } from "react";
interface Row { index:number; top:number; height:number; text:string; heading:boolean; font:number }
interface Position { index:number; text:string; fraction:number; updatedAt:number }
const KEY="guizhi-snapshot-reading-v1";
function read(): Record<string,Position> {
  try { const value=JSON.parse(localStorage.getItem(KEY)||"{}"); return value && typeof value==="object" && !Array.isArray(value) ? value : {}; } catch { return {}; }
}
function valid(value:Position):boolean { return !!value && Number.isSafeInteger(value.index) && value.index>=0 && typeof value.text==="string" && Number.isFinite(value.fraction) && value.fraction>=0 && value.fraction<=100; }
/** 原文按版本记住段落和段内比例；图片/宽度变化后重新计算该段落位置。 */
export function useSnapshotReader(frame:RefObject<HTMLIFrameElement>,scroll:RefObject<HTMLDivElement>,instanceId:string|undefined,memoryKey:string,enabled:boolean) {
  const [rows,setRows]=useState<Row[]>([]),[font,setFont]=useState(16),[progress,setProgress]=useState(0),[catalogOpen,setCatalogOpen]=useState(false);
  const [active,setActive]=useState<number|null>(null),[wideRail,setWideRail]=useState(false);
  useEffect(()=>{if(!enabled)return;const host=scroll.current,iframe=frame.current;if(!host||!iframe)return;const observer=new ResizeObserver(()=>setWideRail((host.clientWidth-iframe.getBoundingClientRect().width)/2>=64));observer.observe(host);observer.observe(iframe);return()=>observer.disconnect();},[enabled,frame,scroll]);
  const rowsRef=useRef<Row[]>([]),position=useRef<Position|null>(null),ready=useRef(false);
  useEffect(()=>{
    rowsRef.current=[];setRows([]);ready.current=false;
    const saved=read()[memoryKey];position.current=valid(saved)?saved:null;
    if(!enabled)return;
    let timer:ReturnType<typeof setTimeout>|undefined,restoreFrame=0;
    const persist=()=>{
      if(!position.current)return;
      try {const store=read();store[memoryKey]=position.current;const recent=Object.entries(store).filter(([,v])=>valid(v)).sort((a,b)=>b[1].updatedAt-a[1].updatedAt).slice(0,100);localStorage.setItem(KEY,JSON.stringify(Object.fromEntries(recent)));}catch{/* 阅读不因存储配额失败中断 */}
    };
    const update=()=>{
      const host=scroll.current;if(!host||!ready.current)return;
      const top=Math.max(0,host.scrollTop-12),list=rowsRef.current;
      const row=[...list].reverse().find(row=>row.top<=top+2)||list[0];
      if(host.scrollTop===0)position.current={index:0,text:"",fraction:0,updatedAt:Date.now()};
      else if(row)position.current={index:row.index,text:row.text,fraction:Math.max(0,Math.min(100,(top-row.top)/Math.max(1,row.height))),updatedAt:Date.now()};
      setActive([...list].reverse().find(row=>row.heading&&row.top<=top+64)?.index??null);
      setProgress(Math.round(100*host.scrollTop/Math.max(1,host.scrollHeight-host.clientHeight)));
      clearTimeout(timer);timer=setTimeout(persist,180);
    };
    const receive=(event:MessageEvent)=>{
      if(event.source!==frame.current?.contentWindow||event.origin!=="null"||event.data?.id!==instanceId||event.data.type!=="reader-layout")return;
      const value=event.data.value;
      if(!value||!Array.isArray(value.rows)||value.rows.length>4000||typeof value.ready!=="boolean")return;
      const list:Row[]=value.rows.filter((r:Row)=>r&&Number.isSafeInteger(r.index)&&r.index>=0&&Number.isFinite(r.top)&&r.top>=0&&r.top<=2000000&&Number.isFinite(r.height)&&r.height>=0&&typeof r.text==="string"&&r.text.length<=100&&typeof r.heading==="boolean");
      rowsRef.current=list;setRows(list);
      if(Number.isFinite(value.font))setFont(Math.max(12,Math.min(32,value.font)));
      if(!value.ready)return;
      ready.current=false;cancelAnimationFrame(restoreFrame);
      restoreFrame=requestAnimationFrame(()=>{
        const saved=position.current,host=scroll.current;
        if(saved&&host){const row=list.find(r=>r.index===saved.index&&r.text===saved.text)||list.find(r=>r.text===saved.text);if(!saved.text)host.scrollTop=0;else if(row)host.scrollTop=row.top+row.height*saved.fraction+12;}
        ready.current=true;update();
      });
    };
    const host=scroll.current;
    window.addEventListener("message",receive);host?.addEventListener("scroll",update,{passive:true});window.addEventListener("pagehide",persist);
    return()=>{window.removeEventListener("message",receive);host?.removeEventListener("scroll",update);window.removeEventListener("pagehide",persist);clearTimeout(timer);cancelAnimationFrame(restoreFrame);persist();};
  },[frame,scroll,instanceId,memoryKey,enabled]);
  return {active,wideRail,top:()=>scroll.current?.scrollTo({top:0}),catalogOpen,setCatalogOpen,progress,comfortableWidth:Math.round(Math.max(560,Math.min(900,font*36+48))),headings:rows.filter(row=>row.heading).slice(0,100),jump:(index:number)=>{const row=rowsRef.current.find(row=>row.index===index);if(row)scroll.current?.scrollTo({top:Math.max(0,row.top-24)});}};
}
