import type {
  DiscoveryView,
  PlatformDiscoveryPage,
} from "@guizhi/shared/types";

/** 平台适配器的最小契约；DOM/payload 细节继续由既有平台采集实现负责。 */
export interface DiscoveryCollector {
  collect(view: DiscoveryView, cursor: string | null): Promise<PlatformDiscoveryPage>;
}
