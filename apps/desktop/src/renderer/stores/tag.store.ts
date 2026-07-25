import { create } from "zustand";
import type { CreateTagInput, Tag, UpdateTagInput } from "@guizhi/shared/types";

interface TagState {
  tags: Tag[];
  isLoading: boolean;
  fetchTags: () => Promise<void>;
  createTag: (input: CreateTagInput) => Promise<Tag>;
  updateTag: (id: string, input: UpdateTagInput) => Promise<Tag | null>;
  deleteTag: (id: string) => Promise<boolean>;
}

export const useTagStore = create<TagState>()((set, get) => ({
  tags: [],
  isLoading: false,

  fetchTags: async () => {
    set({ isLoading: true });
    try {
      const tags = await window.api.tag.list();
      set({ tags });
    } catch (error) {
      console.error("加载标签列表失败:", error);
    } finally {
      set({ isLoading: false });
    }
  },

  createTag: async (input) => {
    const created = await window.api.tag.create(input);
    await get().fetchTags();
    return created;
  },

  updateTag: async (id, input) => {
    const updated = await window.api.tag.update(id, input);
    await get().fetchTags();
    return updated;
  },

  deleteTag: async (id) => {
    const removed = await window.api.tag.delete(id);
    await get().fetchTags();
    return removed;
  },
}));
