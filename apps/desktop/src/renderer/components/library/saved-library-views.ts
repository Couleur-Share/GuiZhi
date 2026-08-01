import type { KnowledgeScope } from "@guizhi/shared/types";
import type { LibraryFacetFilters } from "../../stores/knowledge.store";

export interface SavedLibraryView extends LibraryFacetFilters {
  id: string;
  name: string;
  createdAt: number;
}

const STORAGE_KEY = "guizhi-library-smart-views-v1";
const MAX_SAVED_VIEWS = 30;
const SCOPES = new Set<KnowledgeScope>([
  "uncategorized",
  "all",
  "favorites",
  "archived",
  "trash",
]);

function normalize(value: unknown): SavedLibraryView | null {
  if (!value || typeof value !== "object" || Array.isArray(value)) return null;
  const raw = value as Record<string, unknown>;
  if (
    typeof raw.id !== "string" ||
    typeof raw.name !== "string" ||
    !raw.name.trim() ||
    typeof raw.scope !== "string" ||
    !SCOPES.has(raw.scope as KnowledgeScope)
  ) {
    return null;
  }
  const nullableString = (entry: unknown): string | null =>
    typeof entry === "string" && entry ? entry : null;
  return {
    id: raw.id,
    name: raw.name.trim().slice(0, 60),
    scope: raw.scope as KnowledgeScope,
    collectionId: nullableString(raw.collectionId),
    tagId: nullableString(raw.tagId),
    platform: nullableString(raw.platform),
    createdAt:
      typeof raw.createdAt === "number" && Number.isFinite(raw.createdAt)
        ? raw.createdAt
        : 0,
  };
}

export function readSavedLibraryViews(): SavedLibraryView[] {
  try {
    const raw = JSON.parse(localStorage.getItem(STORAGE_KEY) ?? "[]");
    if (!Array.isArray(raw)) return [];
    return raw
      .map(normalize)
      .filter((view): view is SavedLibraryView => view !== null)
      .slice(0, MAX_SAVED_VIEWS);
  } catch {
    return [];
  }
}

export function writeSavedLibraryViews(views: SavedLibraryView[]): void {
  localStorage.setItem(
    STORAGE_KEY,
    JSON.stringify(views.slice(0, MAX_SAVED_VIEWS)),
  );
}

export function createSavedLibraryView(
  name: string,
  filters: LibraryFacetFilters,
): SavedLibraryView | null {
  const trimmed = name.trim().slice(0, 60);
  if (!trimmed) return null;
  return {
    id: crypto.randomUUID(),
    name: trimmed,
    ...filters,
    createdAt: Date.now(),
  };
}
