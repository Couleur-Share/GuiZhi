/** 专题内容与不可变原文分开保存；引用是定位依据，不代表事实已经核实。 */
export interface ThemedEvidence { blockId: string; quote: string; }
export interface ThemedStatement {
  text: string;
  kind: "quote" | "summary" | "inference";
  evidence: ThemedEvidence[];
  emphasis?: string[];
}
export interface ThemedCompositionItem {
  title: string;
  badge?: string;
  tone?: "neutral" | "positive" | "caution";
  body: ThemedStatement;
  metrics?: { label: string; value: ThemedStatement }[];
  takeaway?: ThemedStatement;
}
export interface ThemedCompositionSection {
  title: string;
  label?: string;
  navLabel?: string;
  layout: "cards" | "comparison" | "illustrated" | "prose" | "checklist" | "matrix" | "explorer" | "calculator";
  intro?: ThemedStatement;
  imageId?: string;
  items?: ThemedCompositionItem[];
  columns?: string[];
  rows?: { label: string; cells: ThemedStatement[] }[];
  calculator?: "unit-cost" | "daily-total";
}
export interface ThemedCompositionChapter {
  blockIds: string[];
  palette: "marine" | "amber" | "forest" | "ink";
  category: string;
  displayTitle?: string;
  subtitle: ThemedStatement;
  lead: ThemedStatement;
  imageId?: string;
  sections: ThemedCompositionSection[];
}
export interface ThemedComposition { version: 1; chapters: ThemedCompositionChapter[]; }
