/**
 * 内容面板阅读位置记忆：标签、滚动、讨论搜索词、目录开关。
 * 按条目存 localStorage，坏数据静默丢弃。
 */

export type ReadingPanelTab =
  | "body"
  | "transcript"
  | "images"
  | "recognized"
  | "summary"
  | "replies";

export interface ContentReadingMemory {
  tab: ReadingPanelTab;
  scrollTopByTab: Partial<Record<ReadingPanelTab, number>>;
  repliesQuery?: string;
  catalogOpen?: boolean;
  /** 用于淘汰最旧条目 */
  updatedAt: number;
}

const STORAGE_KEY = "guizhi-content-reading-v1";
const MAX_ENTRIES = 100;

const TABS = new Set<ReadingPanelTab>([
  "body",
  "transcript",
  "images",
  "recognized",
  "summary",
  "replies",
]);

type Store = Record<string, ContentReadingMemory>;

function readStore(): Store {
  try {
    const parsed = JSON.parse(localStorage.getItem(STORAGE_KEY) ?? "null");
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
      return {};
    }
    const out: Store = {};
    for (const [id, value] of Object.entries(parsed)) {
      const entry = normalizeEntry(value);
      if (entry) {
        out[id] = entry;
      }
    }
    return out;
  } catch {
    return {};
  }
}

function normalizeEntry(value: unknown): ContentReadingMemory | null {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    return null;
  }
  const raw = value as Record<string, unknown>;
  if (typeof raw.tab !== "string" || !TABS.has(raw.tab as ReadingPanelTab)) {
    return null;
  }
  const scrollTopByTab: ContentReadingMemory["scrollTopByTab"] = {};
  if (raw.scrollTopByTab && typeof raw.scrollTopByTab === "object") {
    for (const [key, top] of Object.entries(
      raw.scrollTopByTab as Record<string, unknown>,
    )) {
      if (TABS.has(key as ReadingPanelTab) && typeof top === "number" && Number.isFinite(top)) {
        scrollTopByTab[key as ReadingPanelTab] = Math.max(0, top);
      }
    }
  }
  return {
    tab: raw.tab as ReadingPanelTab,
    scrollTopByTab,
    repliesQuery:
      typeof raw.repliesQuery === "string" ? raw.repliesQuery : undefined,
    catalogOpen:
      typeof raw.catalogOpen === "boolean" ? raw.catalogOpen : undefined,
    updatedAt:
      typeof raw.updatedAt === "number" && Number.isFinite(raw.updatedAt)
        ? raw.updatedAt
        : Date.now(),
  };
}

function writeStore(store: Store): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(store));
  } catch (error) {
    console.warn("保存阅读位置失败:", error);
  }
}

function trimStore(store: Store): Store {
  const entries = Object.entries(store).sort(
    (a, b) => b[1].updatedAt - a[1].updatedAt,
  );
  if (entries.length <= MAX_ENTRIES) {
    return store;
  }
  return Object.fromEntries(entries.slice(0, MAX_ENTRIES));
}

export function loadContentReadingMemory(
  itemId: string,
): ContentReadingMemory | null {
  return readStore()[itemId] ?? null;
}

export function saveContentReadingMemory(
  itemId: string,
  memory: Omit<ContentReadingMemory, "updatedAt"> & { updatedAt?: number },
): void {
  const store = readStore();
  store[itemId] = {
    ...memory,
    updatedAt: memory.updatedAt ?? Date.now(),
  };
  writeStore(trimStore(store));
}

export function patchContentReadingMemory(
  itemId: string,
  patch: Partial<Omit<ContentReadingMemory, "updatedAt">>,
): void {
  const prev = loadContentReadingMemory(itemId);
  saveContentReadingMemory(itemId, {
    tab: patch.tab ?? prev?.tab ?? "body",
    scrollTopByTab: {
      ...(prev?.scrollTopByTab ?? {}),
      ...(patch.scrollTopByTab ?? {}),
    },
    repliesQuery:
      patch.repliesQuery !== undefined
        ? patch.repliesQuery
        : prev?.repliesQuery,
    catalogOpen:
      patch.catalogOpen !== undefined ? patch.catalogOpen : prev?.catalogOpen,
  });
}
