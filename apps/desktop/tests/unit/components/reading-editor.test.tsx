import React from "react";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { ReconstructionReader } from "../../../src/renderer/components/themed-reading/ReconstructionReader";
const mocks = vi.hoisted(() => ({store:null as any,toast:vi.fn()}));
vi.mock("../../../src/renderer/stores/knowledge.store",()=>({useKnowledgeStore:{getState:()=>mocks.store}}));
vi.mock("../../../src/renderer/components/ui/Toast",()=>({useToast:()=>({showToast:mocks.toast})}));
vi.mock("../../../src/renderer/components/library/MarkdownPreview",()=>({MarkdownPreview:()=>null}));
vi.mock("../../../src/renderer/components/library/MarkdownEditor",()=>({MarkdownEditor:({value,onChange})=><textarea aria-label="Markdown" value={value} onChange={e=>onChange(e.target.value)} />}));
vi.mock("../../../src/renderer/components/themed-reading/ThemedReadingPane",()=>({ThemedReadingPane:()=>null}));

it("保存失败保留编辑内容并可重试，成功才退出编辑",async()=>{
  const item = {id:"editor-retry",itemType:"forum",content:"## 讨论总结\n\n旧总结\n\n## 正文\n\n主楼\n\n## 讨论（1 条）\n\n回复"} as any;
  const finishEditing=vi.fn(), mode={sourceKind:"summary",active:false,editing:true,finishEditing} as any;
  mocks.store={selectedItem:item,hasUnsavedChanges:true,updateSelected:vi.fn(patch=>{mocks.store.selectedItem={...item,...patch};}),flushPendingSave:vi.fn().mockRejectedValueOnce(new Error("模拟保存失败")).mockImplementation(async()=>{mocks.store.hasUnsavedChanges=false;return true;})};
  render(<ReconstructionReader item={item} mode={mode} scrollRef={{current:null}} findQuery="" findIndex={0} onFindCount={()=>{}} onFindOpen={()=>{}} onModeChange={()=>{}} />);
  fireEvent.change(await screen.findByLabelText("Markdown"),{target:{value:"重写的总结"}});
  fireEvent.click(screen.getByRole("button",{name:"完成编辑"}));
  await screen.findByRole("alert"); expect(finishEditing).not.toHaveBeenCalled();
  expect(screen.getByLabelText("Markdown")).toHaveValue("重写的总结");
  fireEvent.click(screen.getByRole("button",{name:"完成编辑"}));
  await waitFor(()=>expect(finishEditing).toHaveBeenCalledOnce());
  expect(mocks.store.selectedItem.content).toContain("主楼"); expect(mocks.store.selectedItem.content).toContain("回复");
});
