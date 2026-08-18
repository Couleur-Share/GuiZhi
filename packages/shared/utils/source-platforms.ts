/**
 * 采集来源平台归类。侧栏「平台」分区、采集落库与老库回填共用这一份判定。
 *
 * 平台此前只以中文散文的形式存在于正文的 `> 平台：抖音 · …` 引用块里，
 * 查不了也统计不了。这里把它收敛成一个封闭枚举，写进
 * `source_records.platform`。
 *
 * 判定完全由 URL 决定，且复用连接器分流时用的同两个函数
 * （detectVideoPlatform / detectForumPlatform）——采集当时走了哪条抽取
 * 路径，事后回填就归到哪个平台，两边不可能算出不同的结果。
 */
import { detectVideoPlatform } from "./video-platforms";
import { detectForumPlatform } from "./forum-platforms";

/**
 * 平台清单。数组顺序即侧栏展示顺序：专有平台在前，两个兜底桶在后。
 *
 * web 与 local 不是「平台」的严格说法，但少了它们，通用网页与本地文件
 * 导入的条目在这个分区里一条都点不到——分区按来源切分，就不能只切一半。
 */
export const SOURCE_PLATFORMS = [
  "douyin",
  "bilibili",
  "xiaohongshu",
  "youtube",
  "v2ex",
  "nga",
  "linuxdo",
  "appinn",
  "twolibra",
  "web",
  "local",
] as const;

export type SourcePlatform = (typeof SOURCE_PLATFORMS)[number];

const PLATFORM_SET = new Set<string>(SOURCE_PLATFORMS);

export function isSourcePlatform(value: unknown): value is SourcePlatform {
  return typeof value === "string" && PLATFORM_SET.has(value);
}

/**
 * 判定条目来源平台；手工粘贴的文本没有来源，返回 null。
 *
 * sourceKind 取自 source_records.source_type（text / file / url），
 * 必须先按它分支：文件来源的 sourceUri 是绝对路径，交给 URL 解析只会
 * 抛错后落进 web 桶。
 */
export function resolveSourcePlatform(
  sourceKind: string,
  sourceUri: string | null | undefined,
): SourcePlatform | null {
  if (sourceKind === "file") {
    return "local";
  }
  if (sourceKind !== "url" || !sourceUri) {
    return null;
  }
  // 解析不出来的不算网页：旧版迁移带进来的 source_uri 什么都可能是，
  // 一律塞进 web 桶只会让那一组混进一批点不开的东西
  try {
    new URL(sourceUri);
  } catch {
    return null;
  }

  // 直接回传两个检测函数的枚举值而不是就地写死：新增平台时若忘了加进
  // SOURCE_PLATFORMS，这里的返回类型立刻编译不过
  const video = detectVideoPlatform(sourceUri);
  if (video) {
    return video;
  }
  const forum = detectForumPlatform(sourceUri);
  if (forum) {
    return forum.platform;
  }
  return "web";
}
