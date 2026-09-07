import { useEffect, useId, useRef } from "react";
import { ArrowUpIcon } from "lucide-react";
interface Heading { index:number; text:string }
interface Props { headings:Heading[]; active:number|null; progress:number; wide:boolean; open:boolean; onOpen:(open:boolean)=>void; onJump:(index:number)=>void; onTop:()=>void }
/** 借鉴右侧迷你目录交互；浮层在阅读器内部定位，不改变正文几何尺寸。 */
export function SnapshotToc({headings,active,progress,wide,open,onOpen,onJump,onTop}:Props) {
  const root=useRef<HTMLDivElement>(null),timer=useRef<ReturnType<typeof setTimeout>>(),list=useRef<HTMLElement>(null);
  const id=useId();
  useEffect(()=>{
    if(!open)return;
    const close=(e:PointerEvent)=>{if(!root.current?.contains(e.target as Node)&&!(e.target as Element).closest?.('[data-snapshot-toc-trigger]'))onOpen(false);};
    document.addEventListener('pointerdown',close);
    return()=>document.removeEventListener('pointerdown',close);
  },[open,onOpen]);
  useEffect(()=>()=>clearTimeout(timer.current),[]);
  useEffect(()=>{if(open)list.current?.querySelector('[aria-current="location"]')?.scrollIntoView({block:'nearest'});},[active,open]);
  if(!wide&&!open)return null;
  return <div ref={root} data-testid="snapshot-toc" className="absolute bottom-4 right-3 top-4 z-10"
    onMouseEnter={()=>{clearTimeout(timer.current);if(wide)onOpen(true);}}
    onMouseLeave={()=>{timer.current=setTimeout(()=>{if(!root.current?.contains(document.activeElement))onOpen(false);},180);}}
    onBlur={e=>{if(!e.currentTarget.contains(e.relatedTarget as Node))onOpen(false);}}
    onKeyDown={e=>{if(e.key==='Escape'&&open){e.preventDefault();e.stopPropagation();onOpen(false);}}}>
    {wide ? <div className="flex max-h-full w-10 flex-col items-center gap-2 text-muted-foreground">
      <button data-snapshot-toc-trigger aria-label="展开文章目录" aria-expanded={open} aria-controls={id} className="rounded px-1 py-2 text-xs hover:text-foreground focus-visible:ring-2 focus-visible:ring-primary" onClick={()=>onOpen(!open)} onFocus={()=>onOpen(true)}>目录</button>
      <div className="flex min-h-0 max-h-60 w-full flex-col items-center overflow-auto">
        {headings.map(h=><button key={h.index} aria-label={`跳转到${h.text}`} aria-current={active===h.index?'location':undefined} className="group flex h-5 min-h-5 w-9 items-center justify-center rounded focus-visible:ring-2 focus-visible:ring-primary" onFocus={()=>onOpen(true)} onClick={()=>onJump(h.index)}><span className={`h-0.5 rounded transition-colors ${active===h.index?'w-7 bg-primary':'w-4 bg-muted-foreground/35 group-hover:bg-foreground'}`}/></button>)}
      </div>
      <span aria-label={`阅读进度 ${progress}%`} className="text-[10px] tabular-nums">{progress}%</span>
      <button aria-label="返回顶部" className="rounded p-1 hover:bg-accent focus-visible:ring-2 focus-visible:ring-primary" onClick={onTop}><ArrowUpIcon className="h-4 w-4"/></button>
    </div> : null}
    {open ? <nav ref={list} id={id} aria-label="文章章节目录" className={`absolute top-0 w-60 max-w-[calc(100vw-3rem)] overflow-auto rounded-xl border border-border bg-popover p-2 text-popover-foreground shadow-lg ${wide?'right-12':'right-0'}`} style={{maxHeight:'min(440px, 100%)'}}>
      <div className="mb-1 flex items-center justify-between border-b border-border px-2 pb-2 text-xs"><span>文章目录</span><span className="text-muted-foreground">{progress}%</span></div>
      {headings.length ? headings.map(h=><button key={h.index} aria-current={active===h.index?'location':undefined} className={`block w-full rounded-lg px-2 py-2 text-left text-xs leading-relaxed focus-visible:ring-2 focus-visible:ring-primary ${active===h.index?'bg-accent text-primary':'hover:bg-accent'}`} onClick={()=>{onJump(h.index);onOpen(false);}}>{h.text}</button>):<p className="p-2 text-xs text-muted-foreground">未识别到明确章节标题</p>}
      <button className="mt-1 w-full rounded border-t border-border p-2 text-left text-xs hover:bg-accent" onClick={()=>{onTop();onOpen(false);}}>返回顶部</button>
    </nav>:null}
  </div>;
}
