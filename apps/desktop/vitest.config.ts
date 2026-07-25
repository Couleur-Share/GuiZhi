/// <reference types="vitest" />
import { defineConfig } from "vitest/config";
import path from "path";

export default defineConfig({
  test: {
    globals: true,
    environment: "jsdom",
    setupFiles: "./tests/setup.ts",
    include: ["tests/**/*.{test,spec}.{ts,tsx}"],
    exclude: ["tests/e2e/**/*", "node_modules/**/*"],
    testTimeout: 30_000,
    hookTimeout: 30_000,
    minWorkers: 2,
    maxWorkers: 4,
  },
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "src"),
      "@renderer": path.resolve(__dirname, "src/renderer"),
      "@guizhi/core": path.resolve(__dirname, "../../packages/core/src"),
      "@shared": path.resolve(__dirname, "../../packages/shared"),
      "@guizhi/shared": path.resolve(__dirname, "../../packages/shared"),
      "@guizhi/db": path.resolve(__dirname, "../../packages/db/src"),
    },
  },
});
