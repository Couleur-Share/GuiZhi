import type { WikiApplyCompilationInput } from './wiki';
export const WIKI_COMPILER_VERSION = 'fulltext-v2';
export type WikiCandidateState = 'ready' | 'review' | 'waiting' | 'upgrade' | 'current' | 'excluded';
export interface WikiBlock { key: string; hash: string; field: 'title' | 'content' | 'transcript'; section: string; start: number; end: number; text: string; }
export interface WikiCandidate { id: string; title: string; fingerprint: string; state: WikiCandidateState; reason?: string; blocks: number; reusable: number; }
export interface WikiCompilePreview { entries: WikiCandidate[]; counts: Record<WikiCandidateState, number>; requests: { min: number; max: number }; }
export type WikiJobStatus = 'running' | 'paused' | 'cancelled' | 'interrupted' | 'failed' | 'completed';
export interface WikiCompileJob { id: string; model: string; status: WikiJobStatus; total: number; completed: number; failed: number; error?: string; createdAt: number; }
export interface WikiCompileWork { jobId: string; itemId: string; title: string; fingerprint: string; block: WikiBlock; }
export type WikiContributions = WikiApplyCompilationInput['pages'];

export type WikiCompilerCommand =
  | { action: 'preview'; model: string; ids?: string[] }
  | { action: 'start'; model: string; selected: { id: string; fingerprint: string }[]; allowUpgrade?: boolean }
  | { action: 'next'; id: string }
  | { action: 'finish'; work: WikiCompileWork; contributions?: WikiContributions; error?: string }
  | { action: 'control'; id: string; status: 'paused' | 'cancelled' | 'running' }
  | { action: 'jobs' }
  | { action: 'suggestion'; pageId: string; accept?: boolean; token?: string };
