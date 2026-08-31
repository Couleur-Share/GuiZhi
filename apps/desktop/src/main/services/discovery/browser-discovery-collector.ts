import type { DiscoveryView } from "@guizhi/shared/types";
import type { BrowserCaptureService } from "../platform-capture/browser-capture";
import type { DiscoveryCollector } from "./discovery-collector";

export class BrowserDiscoveryCollector implements DiscoveryCollector {
  constructor(private readonly browser: BrowserCaptureService) {}

  collect(view: DiscoveryView, cursor: string | null) {
    return view.mode === "creator"
      ? this.browser.discoverCreator({
          platform: view.platform,
          url: view.query,
          cursor,
          limit: 20,
        })
      : this.browser.search({
          platform: view.platform,
          keyword: view.query,
          cursor,
          limit: 20,
        });
  }
}
