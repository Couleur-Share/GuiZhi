import { defineConfig } from "tsup";
export default defineConfig({ entry: ["src/main.ts", "src/admin.ts", "src/backup.ts", "src/reading-main.ts"], format: ["esm"], platform: "node", target: "node24", removeNodeProtocol: false, noExternal: ["@guizhi/shared"], clean: true });
