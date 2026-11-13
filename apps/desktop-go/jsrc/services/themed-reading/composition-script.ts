import { createHash } from "node:crypto";

/** 固定离线交互：原文溯源、内容选择和两个纯算术工具；不解释模型代码。 */
export const COMPOSITION_SCRIPT = `(()=>{
 const root=document.documentElement;
 const reveal=target=>{let node=target;while(node){if(node.tagName==='DETAILS')node.open=true;if(node.classList?.contains('gz-panel'))node.hidden=false;node=node.parentElement;}};
 document.addEventListener('click',event=>{
  const link=event.target.closest('a[href^="#"]');
  if(link){const target=document.getElementById(link.getAttribute('href').slice(1));if(target){reveal(target);document.querySelectorAll('[data-gz-target]').forEach(n=>n.removeAttribute('data-gz-target'));if(target.classList.contains('gz-original-block'))target.setAttribute('data-gz-target','');}}
  const choice=event.target.closest('button[data-gz-choice]');
  if(choice){const tool=choice.closest('.gz-tool');tool.querySelectorAll('[data-gz-choice]').forEach(n=>n.setAttribute('aria-pressed',String(n===choice)));tool.querySelectorAll('.gz-panel').forEach(n=>n.hidden=n.id!==choice.getAttribute('aria-controls'));}
  if(event.target.closest('.gz-theme')&&!root.dataset.instance){const dark=root.dataset.theme?root.dataset.theme==='dark':matchMedia('(prefers-color-scheme:dark)').matches;root.dataset.theme=dark?'light':'dark';}
 },true);
 document.querySelectorAll('[data-gz-calculator]').forEach(tool=>{
  const calculate=()=>{
   const inputs=[...tool.querySelectorAll('input')],out=tool.querySelector('output');
   if(inputs.some(n=>n.value.trim()==='')){out.textContent='填写上方数值后显示计算结果';return;}
   const values=inputs.map(n=>Number(n.value));
   if(values.some(v=>!Number.isFinite(v)||v<0||v>1000000)){out.textContent='请输入 0 到 1,000,000 之间的有效数值';return;}
   const format=v=>new Intl.NumberFormat('zh-CN',{maximumFractionDigits:2}).format(v);
   if(tool.dataset.gzCalculator==='unit-cost'){
    if(values[1]<=0||!Number.isInteger(values[1])){out.textContent='每份数量须为正整数，不能为零';return;}
    out.textContent='每单位 '+format(values[0]/values[1])+' 元 · 每日 '+format(values[0]/values[1]*values[2])+' 元';
   }else out.textContent='每天合计 '+format((values[0]+values[1])*values[2])+' mg（成分 A + 成分 B）';
  };
  tool.addEventListener('input',calculate);calculate();
 });
 window.addEventListener('message',event=>{const d=event.data;if(event.source!==parent||!root.dataset.instance||d?.id!==root.dataset.instance)return;if(d.type==='find'&&typeof d.value?.query==='string'&&d.value.query.trim()&&d.value.query.length<=1000)document.querySelectorAll('.gz-panel,.gz-original').forEach(reveal);},true);
 document.querySelectorAll('.gz-explorer').forEach(tool=>{tool.querySelectorAll('.gz-panel').forEach((node,index)=>node.hidden=index!==0);});
})();`;
export const COMPOSITION_SCRIPT_HASH = createHash("sha256").update(COMPOSITION_SCRIPT).digest("base64");
