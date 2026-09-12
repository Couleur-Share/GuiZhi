import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { SourceCaptureRevisions } from '../../../src/renderer/components/library/SourceCaptureRevisions';
import { useKnowledgeStore } from '../../../src/renderer/stores/knowledge.store';
import { heavyItem } from '../../helpers/heavy-user';
const updateDraft = vi.fn();
beforeEach(() => {
  updateDraft.mockReset();
  useKnowledgeStore.setState({ selectedId:'a', selectedItem:heavyItem('a'), entries:[], flushPendingSave:vi.fn(async()=>true), updateSelected:updateDraft });
  window.api.knowledge = { ...window.api.knowledge,
    selection:vi.fn(async()=>({ok:true,versions:[{id:'revision',title:'来源更新',content:'残缺的新正文',transcript:null,reviewJson:'["文字稿缺失"]',capturedAt:1}]})),
    update:vi.fn(async()=>({...heavyItem('a'),reviewStatus:'needs_review',reviewReasons:['文字稿缺失']})),
  };
});
afterEach(cleanup);
it('采用残缺版本前标记待复核，避免新正文被后台当作完整资料', async () => {
  render(<SourceCaptureRevisions itemId="a"/>);
  const button=await screen.findByRole('button',{name:'采用此版本正文到草稿',hidden:true});
  await act(async()=>{fireEvent.click(button);});
  expect(window.api.knowledge.update).toHaveBeenCalledWith('a',{reviewStatus:'needs_review',reviewReasons:['文字稿缺失']});
  expect(useKnowledgeStore.getState().selectedItem?.reviewStatus).toBe('needs_review');
  expect(updateDraft).toHaveBeenCalledWith({content:'残缺的新正文'});
});
it('复核标记写入失败时不采用正文，原草稿可继续使用', async () => {
  vi.mocked(window.api.knowledge.update).mockRejectedValue(new Error('磁盘已满'));
  render(<SourceCaptureRevisions itemId="a"/>);
  const button=await screen.findByRole('button',{name:'采用此版本正文到草稿',hidden:true});
  await act(async()=>{fireEvent.click(button);});
  expect(updateDraft).not.toHaveBeenCalled(); expect(useKnowledgeStore.getState().selectedItem?.content).toBe('原文');
});
