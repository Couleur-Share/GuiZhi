// 论坛差分基线的打包入口：只再导出要对拍的真实实现。
export * from "../../packages/shared/utils/forum-note";
export { sanitizeForumSummary, needsAiTitle, splitReplyChunks, generateForumSummary } from "../../apps/desktop/src/main/services/import/forum-summary";
export { fetchV2exThread } from "../../apps/desktop/src/main/services/import/v2ex";
export { fetchAppinnThread } from "../../apps/desktop/src/main/services/import/appinn";
export { fetchLinuxdoThread } from "../../apps/desktop/src/main/services/import/linuxdo";
export { fetchTwolibraThread } from "../../apps/desktop/src/main/services/import/twolibra";
export { extractForumPost, buildForumMetaBlock } from "../../apps/desktop/src/main/services/import/forum-post";
