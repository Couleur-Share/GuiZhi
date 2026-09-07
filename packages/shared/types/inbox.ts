export type InboxItem =
  | {
      kind: "review-required";
      id: string;
      itemId: string;
      title: string;
      reasons: string[];
      createdAt: number;
    }
  | {
      kind: "unclassified";
      id: string;
      itemId: string;
      title: string;
      createdAt: number;
    }
  | {
      kind: "import-issue";
      id: string;
      taskId: string;
      title: string;
      status: "failed" | "duplicate" | "completed";
      message: string;
      resultItemId: string | null;
      duplicateItemId: string | null;
      createdAt: number;
    }
  | {
      kind: "discovery-candidate";
      id: string;
      viewId: string;
      externalId: string;
      title: string;
      createdAt: number;
    }
  | {
      kind: "semantic-pending" | "wiki-pending";
      id: string;
      count: number;
      createdAt: number;
    };

export type InboxItemKind = InboxItem["kind"];

export interface InboxListResult {
  items: InboxItem[];
  /** 各分组涉及的数量；后台任务按内容条数统计。 */
  counts: Record<InboxItemKind, number>;
  /** 需人工处理的内容去重总数，不含 Wiki 编译、语义索引。 */
  total: number;
}

export interface InboxOrganizeInput {
  itemIds: string[];
  collectionId?: string | null;
  addTagNames?: string[];
}

/** 主进程仅向 AI 暴露已选未分类条目的有界摘要。 */
export interface InboxAiClassificationSource {
  itemId: string;
  title: string;
  excerpt: string;
}

export interface InboxAiClassificationAssignment {
  itemId: string;
  collectionName: string;
}

export interface InboxAiClassificationApplyInput {
  assignments: InboxAiClassificationAssignment[];
}

export interface InboxAiClassificationApplyResult {
  classified: number;
  skipped: number;
  createdCollectionNames: string[];
}
