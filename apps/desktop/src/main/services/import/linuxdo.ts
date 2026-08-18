/** LINUX DO 的 Discourse 站点配置与登录态降级。 */
import { linuxdoCanonicalUrl } from "@guizhi/shared/utils/forum-platforms";
import {
  fetchDiscourseThread,
  type DiscourseDeps,
  type DiscourseSite,
} from "./discourse";

const LINUXDO_SITE: DiscourseSite = {
  platform: "linuxdo",
  origin: "https://linux.do",
  label: "LINUX DO",
  canonicalUrl: linuxdoCanonicalUrl,
  forbiddenMessage:
    "LINUX DO 访问被 Cloudflare 拦截，请先在「设置 → 采集」完成验证",
  unauthorizedMessage:
    "该帖子需要登录后才能查看，请先在「设置 → 采集」登录 LINUX DO",
};

export interface LinuxdoDeps extends DiscourseDeps {}

export function fetchLinuxdoThread(
  topicId: string,
  deps: LinuxdoDeps = {},
  signal?: AbortSignal,
) {
  return fetchDiscourseThread(topicId, LINUXDO_SITE, deps, signal);
}
