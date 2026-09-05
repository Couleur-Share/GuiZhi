import type { PlatformCapturePlatform } from "@guizhi/shared/types";

export const LOGIN_COOKIE_NAMES: Record<
  PlatformCapturePlatform,
  readonly string[]
> = {
  xiaohongshu: ["web_session", "web_session_v2"],
  douyin: [
    "LOGIN_STATUS",
    "sessionid",
    "sessionid_ss",
    "sid_guard",
    "sid_ucp_virtual",
  ],
  linuxdo: ["_t"],
};

export const LOGIN_FLOW_DOMAINS: Record<
  PlatformCapturePlatform,
  readonly string[]
> = {
  xiaohongshu: ["xiaohongshu.com"],
  // 消费端搜索页的验证码 iframe；与创作者中心登录使用不同的验证入口。
  douyin: ["douyin.com", "iesdouyin.com", "zijieapi.com", "snssdk.com", "rmc.bytedance.com"],
  linuxdo: [
    "linux.do",
    "cloudflare.com",
    "challenges.cloudflare.com",
    "github.com",
    "google.com",
  ],
};

export const RESOURCE_DOMAINS: Record<
  PlatformCapturePlatform,
  readonly string[]
> = {
  xiaohongshu: ["xhscdn.com", "xhsimg.com"],
  douyin: [
    // 只放行页面运行必需的字节官方资源域；遥测域刻意不在白名单内。
    "douyinstatic.com",
    "bytetos.com",
    "bytescm.com",
    "bytegoofy.com",
    "douyinvod.com",
    "douyinpic.com",
    "byteimg.com",
    "byted-static.com",
    "zijieapi.com",
    "snssdk.com",
    // 创作者中心官方登录组件的前端资源，只放行实测必需的精确主机。
    "lf-ucenter-web.yhgfb-cn-static.com",
    // 创作者中心 sdk-glue 1.0.0.62 的验证组件与设备安全脚本备用源。
    // 扫码确认后可能继续校验；拦截它们会让官方验证流程无法完成。
    "lf-rc1.yhgfb-cn-static.com",
    "lf-rc2.yhgfb-cn-static.com",
    "lf-headquarters-speed.yhgfb-cn-static.com",
    // 创作者中心 checkWebId 使用的设备标识服务，保留精确主机边界。
    "ttwid.bytedance.com",
  ],
  linuxdo: [
    "linux.do",
    "cdn.linux.do",
    "cloudflare.com",
    "challenges.cloudflare.com",
    "cloudflareinsights.com",
    "cf-assets.net",
    "discourse.org",
    "discourse-cdn.com",
    "amazonaws.com",
    "googleapis.com",
    "gstatic.com",
    "jsdelivr.net",
    "unpkg.com",
    "hcaptcha.com",
  ],
};
