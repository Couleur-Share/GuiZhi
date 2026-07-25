// 一次性脚本：M0 清理 locale 文件（跑完即删）
import fs from "node:fs";
import path from "node:path";

const localesDir = path.resolve("apps/desktop/src/renderer/i18n/locales");

const REMOVED_NAMESPACES = [
  "prompt",
  "agents",
  "rules",
  "folder",
  "skill",
  "quickAdd",
  "imageReverse",
  "recovery",
  "mcp",
  "plugin",
  "generation",
  "auth",
  "resources",
  "import",
  "filter",
];

const ADDITIONS = {
  zh: {
    nav: {
      library: "知识库",
      ask: "AI 问答",
      wiki: "Wiki",
      imports: "导入",
      panelPlaceholder: "导航内容将在后续版本中提供",
      modulePlaceholder: "该模块正在迁移中，将在后续里程碑中提供。",
    },
    header: {
      lightMode: "浅色模式",
      darkMode: "深色模式",
      clearSearch: "清除搜索",
    },
    settings: {
      dataPlaceholder: "备份、恢复与数据迁移功能正在迁移中，将在后续版本提供。",
      aiWorkbenchRouteEmbedding: "Embedding",
      aiWorkbenchRouteEmbeddingDesc: "语义检索使用的向量模型",
      aiWorkbenchBadgeEmbedding: "向量",
      aiWorkbenchEmbeddingModel: "Embedding 模型",
    },
  },
  en: {
    nav: {
      library: "Library",
      ask: "Ask AI",
      wiki: "Wiki",
      imports: "Imports",
      panelPlaceholder: "Navigation will arrive in a later milestone",
      modulePlaceholder: "This module is being migrated and will arrive in a later milestone.",
    },
    header: {
      lightMode: "Light mode",
      darkMode: "Dark mode",
      clearSearch: "Clear search",
    },
    settings: {
      dataPlaceholder: "Backup, restore and data migration are being migrated and will arrive in a later release.",
      aiWorkbenchRouteEmbedding: "Embedding",
      aiWorkbenchRouteEmbeddingDesc: "Vector model used for semantic search",
      aiWorkbenchBadgeEmbedding: "Embedding",
      aiWorkbenchEmbeddingModel: "Embedding model",
    },
  },
};

function deepMerge(target, source) {
  for (const [key, value] of Object.entries(source)) {
    if (value && typeof value === "object" && !Array.isArray(value)) {
      target[key] = deepMerge(target[key] ?? {}, value);
    } else {
      target[key] = value;
    }
  }
  return target;
}

for (const lang of ["zh", "en"]) {
  const filePath = path.join(localesDir, `${lang}.json`);
  const data = JSON.parse(fs.readFileSync(filePath, "utf8"));
  for (const ns of REMOVED_NAMESPACES) {
    delete data[ns];
  }
  deepMerge(data, ADDITIONS[lang]);
  fs.writeFileSync(filePath, JSON.stringify(data, null, 2) + "\n", "utf8");
  console.log(`${lang}.json cleaned: ${Object.keys(data).length} namespaces`);
}
