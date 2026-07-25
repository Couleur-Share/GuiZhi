// 一次性脚本：M0 品牌替换（跑完即删）
import fs from "node:fs";
import { execSync } from "node:child_process";

const REPLACEMENTS = [
  ["https://github.com/legeling/PromptHub", "https://github.com/Couleur-Share/GuiZhi"],
  ["github.com/legeling/PromptHub", "github.com/Couleur-Share/GuiZhi"],
  ["repos/legeling/PromptHub", "repos/Couleur-Share/GuiZhi"],
  ["prompthub-table-config", "guizhi-table-config"],
  ["prompthub-ai-config", "guizhi-ai-config"],
  ["prompthub.database-client-lock.exit-cleanup", "guizhi.database-client-lock.exit-cleanup"],
  ["PromptHub-Updater", "GuiZhi-Updater"],
  ["PromptHub/image-download", "GuiZhi/image-download"],
  ["PromptHub/1.0", "GuiZhi/1.0"],
  ["PROMPTHUB_OPEN_DEVTOOLS", "GUIZHI_OPEN_DEVTOOLS"],
  ["PROMPTHUB_INSTALL_STATE_KEY", "GUIZHI_INSTALL_STATE_KEY"],
  ["Software\\\\PromptHub\\\\InstallerState", "Software\\\\GuiZhi\\\\InstallerState"],
  ["brew upgrade --cask prompthub", "brew upgrade --cask guizhi"],
  ["'brew upgrade --cask guizhi'", "'brew upgrade --cask guizhi'"],
  ["PromptHub", "GuiZhi"],
  ["prompthub", "guizhi"],
];

const files = execSync(
  'rg -l -i prompthub -g "!node_modules" -g "!pnpm-lock.yaml" -g "!*.log" -g "!README.md" -g "!AGENTS.md" -g "!scripts/rebrand.mjs" -g "!resources/tray/**" -g "!electron-builder.config.cjs"',
  { encoding: "utf8", cwd: process.cwd() },
)
  .trim()
  .split("\n")
  .filter(Boolean);

for (const file of files) {
  let text = fs.readFileSync(file, "utf8");
  for (const [from, to] of REPLACEMENTS) {
    text = text.split(from).join(to);
  }
  fs.writeFileSync(file, text, "utf8");
  console.log(`rebranded: ${file}`);
}
