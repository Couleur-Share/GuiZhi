import type { API } from "../preload";

declare global {
  interface Window {
    api: API;
    __GUIZHI_WEB__?: boolean;
  }
}

export {};
