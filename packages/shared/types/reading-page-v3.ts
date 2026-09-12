import type {
  ThemedReadingRequest,
  ThemedReadingVersion,
} from "./themed-reading";

export interface ReadingIssue {
  kind: "format" | "content" | "resource" | "script" | "runtime" | "security";
  message: string;
  unit?: string;
  line?: number;
}
export interface ReadingScriptModule {
  id: string;
  rootId?: string;
  code: string;
  status: "pending" | "ready" | "failed";
  error?: string;
}
export type ReadingLibrary = "echarts" | "mermaid" | "animation";
export interface ReadingPageSection {
  id: string;
  html: string;
}
/** 候选与正式设计分开；检查点允许尚未通过页面校验，但不允许无界数据。 */
export interface ReadingGeneration {
  route: "short" | "long" | "research" | "redesign";
  sections: ReadingPageSection[];
  css: string;
  candidateCss?: string;
  editorCandidate?: string;
  scripts: ReadingScriptModule[];
  libraries: ReadingLibrary[];
  revision: number;
  done: boolean;
  repairs: Record<string, number>;
  issues: ReadingIssue[];
  candidates?: Record<string, string>;
  sectionOrder?: string[];
  notesByPart?: Record<string, string>;
  reducedNotes?: Record<string, string>;
  reusedChapters?: number;
  draftBySection?: Record<
    string,
    import("./reading-reconstruction").ReadingDraftSection
  >;
  limited?: boolean;
  researchRoundLimit?: number;
  evidence?: { id: string; text: string; sections?: number[] }[];
}
export type ReadingPageRecord =
  | { type: "meta"; css: string; libraries?: ReadingLibrary[] }
  | { type: "section"; id: string; html: string }
  | { type: "script"; id: string; rootId?: string; code: string }
  | { type: "done" };

export interface ReadingViewRequest extends ThemedReadingRequest {
  versionId: string;
  preview?: boolean;
  scriptsEnabled?: boolean;
}
export interface ReadingViewBounds {
  x: number;
  y: number;
  width: number;
  height: number;
  visible: boolean;
}
export type ReadingViewCommand =
  | {
      type: "appearance";
      theme: "light" | "dark";
      fontSize: number;
      fontFamily?: string;
    }
  | { type: "find"; query: string; index: number; requestId?: string }
  | { type: "anchor"; id: string }
  | { type: "scroll"; top: number }
  | { type: "interaction"; enabled: boolean };
export interface ReadingViewEvent {
  viewId: string;
  type: "ready" | "layout" | "selection" | "find" | "image" | "fault" | "key";
  value?: unknown;
}
export interface ReadingPreview {
  page: ThemedReadingVersion;
  revision: number;
}
export interface ReadingViewResult {
  success: boolean;
  error?: string;
  viewId?: string;
  preview?: ReadingPreview;
}
