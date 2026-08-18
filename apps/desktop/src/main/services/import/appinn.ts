/** 小众软件官方论坛（Discourse）帖子抓取。公开主题无需登录态。 */
import { appinnCanonicalUrl } from "@guizhi/shared/utils/forum-platforms";
import {
  fetchDiscourseThread,
  type DiscourseDeps,
  type DiscourseSite,
} from "./discourse";

const APPINN_SITE: DiscourseSite = {
  platform: "appinn",
  origin: "https://meta.appinn.net",
  label: "小众软件论坛",
  canonicalUrl: appinnCanonicalUrl,
};

export interface AppinnDeps extends DiscourseDeps {}

export function fetchAppinnThread(
  topicId: string,
  deps: AppinnDeps = {},
  signal?: AbortSignal,
) {
  return fetchDiscourseThread(topicId, APPINN_SITE, deps, signal);
}
