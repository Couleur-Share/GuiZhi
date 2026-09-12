import fs from "node:fs/promises";
import path from "node:path";
import { readingSvgFixture } from "../tests/unit/reading-svg-fixture";
import { themedReadingDocument } from "../src/main/services/themed-reading/document";

const directory = path.resolve(process.env.GUIZHI_SVG_FIXTURES ?? "../../artifacts/reading-svg");
await fs.mkdir(directory, { recursive: true });
const page = readingSvgFixture();
await fs.writeFile(path.join(directory, "offline.html"), themedReadingDocument(page));
await fs.writeFile(path.join(directory, "embedded.html"), themedReadingDocument(page, "svg-fixture"));
console.log(`SVG 隔离素材已写入 ${directory}；模型调用 0 次`);
