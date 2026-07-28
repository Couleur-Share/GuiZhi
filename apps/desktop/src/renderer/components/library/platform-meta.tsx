import { GlobeIcon, HardDriveIcon, CircleHelpIcon } from "lucide-react";
import type { SourcePlatform } from "@guizhi/shared/utils/source-platforms";
import {
  BilibiliLogo,
  DouyinLogo,
  V2exLogo,
  XiaohongshuLogo,
  YoutubeLogo,
  type PlatformLogo,
} from "../ui/PlatformLogos";

export interface SourcePlatformMeta {
  labelKey: string;
  fallback: string;
  Icon: PlatformLogo;
  /** 图标着色的 text-* class，由调用方拼进 className */
  colorClass: string;
}

/**
 * 侧栏「平台」分区与表格「来源」列的图标与名称。
 *
 * 五个专有平台用各自的品牌 logo（`ui/PlatformLogos.tsx`）而不是形态近似的通用
 * 图标：音符、电视、书这类替身要先读懂旁边的文字才对得上号，等于没帮上忙，而
 * 这一列的全部用处就是不看文字也能扫出哪行是哪个平台。
 *
 * 着色分两类。有彩色标准色的用品牌色——这里刻意不走语义令牌，品牌色不是主题的
 * 一部分，随主题漂移就不再是那个平台的颜色了。抖音（#000000）与 V2EX（#1F1F1F）
 * 的标准色是近黑，写死会在深色主题下整个消失，改用 `text-foreground` 跟着主题
 * 走：这两个 mark 本来给人的印象就是中性色，浅色下发黑、深色下发白都不违和。
 *
 * 网页与本地文件是兜底桶、没有品牌，保留 lucide 通用图标并继承行的文字色，
 * 比品牌 logo 淡一档，正好也符合它们「归不了类才落这儿」的地位。
 */
export const SOURCE_PLATFORM_META: Record<SourcePlatform, SourcePlatformMeta> =
  {
    douyin: {
      labelKey: "library.platformDouyin",
      fallback: "抖音",
      Icon: DouyinLogo,
      colorClass: "text-foreground",
    },
    bilibili: {
      labelKey: "library.platformBilibili",
      fallback: "哔哩哔哩",
      Icon: BilibiliLogo,
      colorClass: "text-[#00A1D6]",
    },
    xiaohongshu: {
      labelKey: "library.platformXiaohongshu",
      fallback: "小红书",
      Icon: XiaohongshuLogo,
      colorClass: "text-[#FF2442]",
    },
    youtube: {
      labelKey: "library.platformYoutube",
      fallback: "YouTube",
      Icon: YoutubeLogo,
      colorClass: "text-[#FF0000]",
    },
    v2ex: {
      labelKey: "library.platformV2ex",
      fallback: "V2EX",
      Icon: V2exLogo,
      colorClass: "text-foreground",
    },
    web: {
      labelKey: "library.platformWeb",
      fallback: "网页",
      Icon: GlobeIcon,
      colorClass: "text-current",
    },
    local: {
      labelKey: "library.platformLocal",
      fallback: "本地文件",
      Icon: HardDriveIcon,
      colorClass: "text-current",
    },
  };

const UNKNOWN_PLATFORM_META: SourcePlatformMeta = {
  labelKey: "library.platformUnknown",
  fallback: "未知来源",
  Icon: CircleHelpIcon,
  colorClass: "text-current",
};

/**
 * 平台查表。形参写成 string 而不是联合类型，理由与 `getItemTypeMeta` 相同：
 * 值直接来自数据库，新版本写入的平台在旧版本里查不到，类型系统认为不可能，
 * 运行时却真会发生——而这里一抛异常，整个知识库列表就白屏了。
 */
export function getSourcePlatformMeta(platform: string): SourcePlatformMeta {
  return Object.hasOwn(SOURCE_PLATFORM_META, platform)
    ? (SOURCE_PLATFORM_META as Record<string, SourcePlatformMeta>)[platform]
    : UNKNOWN_PLATFORM_META;
}

/**
 * 平台图标。着色 class 与图标绑在一起交给这里拼，调用方只管尺寸——分散到各处
 * 自己拼的话，漏掉 colorClass 的表现是那一个平台悄悄退回黑白，不会报错。
 */
export function PlatformIcon({
  platform,
  className,
}: {
  platform: string;
  /** 只管尺寸，颜色下面接上——写成必填是为了不用在运行时防 undefined 拼进 class */
  className: string;
}) {
  const { Icon, colorClass } = getSourcePlatformMeta(platform);
  return <Icon className={`${className} ${colorClass}`} />;
}
