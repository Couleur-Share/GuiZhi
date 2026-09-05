import { defineConfig } from "@playwright/test";
export default defineConfig({ testDir: "tests", use: { baseURL: "http://127.0.0.1:4178", headless: true, locale: "zh-CN", viewport: { width: 390, height: 844 } },
  webServer: { command: "pnpm exec vite preview --host 127.0.0.1 --port 4178", port: 4178, reuseExistingServer: false }, reporter: "list" });
