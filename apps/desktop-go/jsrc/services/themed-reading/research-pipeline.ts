import type { ReadingReconstruction, ReadingResearchBatch } from "@guizhi/shared/types/reading-reconstruction";

const batches = (state: ReadingReconstruction): ReadingResearchBatch[] => [state, ...(state.researchBatches ?? [])];

/** 空搜索结果可以在用户继续时重试；已有结果和已通过的检查点保持不变。 */
export function prepareResearchRetry(state: ReadingReconstruction) {
  for (const batch of batches(state)) if (!batch.researchReview) {
    for (const query of batch.queries) if (query.done && !query.results.length) query.done = false;
  }
}
