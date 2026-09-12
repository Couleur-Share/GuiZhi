/** 参考设计的真实内容样例：原文与图片取自已有只读夹具，专题内容逐条绑定原文。 */
import fs from "node:fs/promises";
import path from "node:path";
import { parseHTML } from "linkedom";
import type { ThemedCompositionChapter, ThemedCompositionItem, ThemedCompositionSection, ThemedStatement } from "@guizhi/shared/types/themed-composition";
import type { ThemedReadingVersion } from "@guizhi/shared/types/themed-reading";
import { themedReadingDocument } from "../src/main/services/themed-reading/document";
import { validateThemedReadingVersion } from "@guizhi/db/themed-reading";

async function main() {
const output = path.resolve("../../artifacts/themed-reading/composition");
const input = JSON.parse(await fs.readFile(path.resolve("../../artifacts/themed-reading/editorial/fixtures.json"), "utf8"));
const results = [];
await fs.mkdir(output, { recursive: true });
for (const fixture of input.fixtures) {
  const page = fixture.page as ThemedReadingVersion;
  const original = parseHTML(await fs.readFile(fixture.files.offline, "utf8")).document;
  const urls: Record<string, string> = {};
  [...original.querySelectorAll("img")].forEach((image, i) => { urls[page.assets[i].id] = image.getAttribute("src"); });
  for (const asset of page.assets) { asset.prompt ??= ""; asset.aspectRatio ??= "16:9"; asset.sha256 = fixture.images[page.assets.indexOf(asset)].sha256; asset.bytes = Buffer.from(urls[asset.id].split(",")[1], "base64").length; }
  const plain = (n: number) => page.source.blocks[n].text.replace(/\s+/gu, " ").trim();
  const s = (n: number, text = plain(n), kind: ThemedStatement["kind"] = "summary"): ThemedStatement => ({ text, kind, evidence: [{ blockId: `b${n}`, quote: plain(n).slice(0, 600) }] });
  const item = (n: number, title: string, text: string, extra: Partial<ThemedCompositionItem> = {}): ThemedCompositionItem => ({ title, body: s(n, text), ...extra });
  const metric = (n: number, label: string, value: string) => ({ label, value: s(n, value) });
  let sections: ThemedCompositionSection[], chapter: ThemedCompositionChapter;
  if (fixture.id === "fish-oil") {
    sections = [
      { title: "服用体验：期待与体感分开看", label: "社群反馈", layout: "cards", items: [
        item(10, "睡眠与记忆", "原帖中，多位网友服用半年到一年仍无明显体感，失眠照旧。这是社群体验汇总。", { badge: "多人反馈无感", tone: "caution" }),
        item(6, "甘油三酯指标", "一位用户反馈服用挪威小鱼后，血脂 TG 指标确实下降；帖子没有提供统一的检验数据或对照条件。", { badge: "个体指标反馈", tone: "neutral" }),
        item(10, "健身恢复", "原帖有一人认为健身后的恢复可能有点帮助，表述本身保留了不确定性。", { badge: "个体体验", tone: "neutral" }),
        item(10, "其他补充剂的讨论", "讨论还提到褪黑素、辅酶 Q10、镁和维生素等。有人报告 Q10 更明显，也有人反馈心跳加快；完整表述可在原文核对。", { badge: "反馈并不一致", tone: "caution" }),
      ] },
      { title: "吃鱼还是补剂？对照日常条件", label: "生活方式", layout: "comparison", items: [
        item(2, "深海鱼与罐头", "多人主张改吃深海鱼、沙丁鱼罐头、青花鱼或生鱼片。有网友吃半年鱼油后改吃鱼，自述体感更好。", { badge: "食物来源", metrics: [metric(2, "帖内成本估计", "天天吃鱼至少十几元")], takeaway: s(2, "原帖的前提是：已经吃够深海鱼，就不必再买补剂。") }),
        item(2, "鱼油胶囊", "另一方看重省事和花费，认为不爱吃鱼时再考虑补剂。双方对是否值得买的判断并不一致。", { badge: "方便与成本", metrics: [metric(2, "帖内成本估计", "约 1 元 / 天")], takeaway: s(12, "分歧集中在方便、价格、食物替代和实际体感。") }),
      ] },
      { title: "品牌体验：参数与口碑放在一起", label: "原帖样本", layout: "cards", items: [
        item(4, "Sports Research", "帖子中至少 4 人推荐。90 粒与 180 粒版本的差异是网友观点，仍需保留这种归属。", { badge: "提及较多", metrics: [metric(4, "价格 / 数量", "约 200 元 / 90 粒")], takeaway: s(12, "原文明确说明：品牌没有压倒性赢家，SR 只是提及最多。") }),
        item(6, "GNC / 挪威小鱼", "GNC 的反馈侧重数量和可用时间；挪威小鱼有 TG 型、服用数量和指标变化的个体记录。", { badge: "不同关注点", metrics: [metric(6, "GNC 4 倍", "约 200 元 / 240 粒"), metric(6, "挪威小鱼", "个体报告 TG 下降")] }),
        item(6, "Viva / Swisse / Mini", "Viva 的反馈是有用但不多；Swisse 有人觉得一般。其他品牌也有无感、偏贵或打嗝腥的反馈。", { badge: "体验有差异", metrics: [metric(6, "4 倍 Mini 单粒", "EPA 360 mg + DHA 243 mg")], takeaway: s(6, "900 mg 是浓缩鱼油总量，帖子同时列出了 EPA 与 DHA 的具体数值。") }),
      ] },
      { title: "选购时核对哪些信息", label: "原帖判断条件", layout: "checklist", items: [
        item(8, "看有效成分", "原帖强调看每天实际 EPA+DHA，而不只看瓶子上的鱼油总量。", { metrics: [metric(8, "关注对象", "EPA + DHA")] }),
        item(8, "看形态与浓度", "讨论倾向高 Omega-3 浓度及 rTG 形态，具体条件来自帖子中的选品观点。", { metrics: [metric(8, "帖内偏好", "Omega-3 > 80% · rTG")] }),
        item(8, "看检测与完整条件", "原帖提到第三方检测、氧化值、重金属、餐后服用，以及同时调整其他油脂摄入的观点。", { metrics: [metric(8, "检测线索", "IFOS 等第三方检测")] }),
      ] },
      { title: "把标签上的数量算清楚", label: "交互换算", layout: "calculator", calculator: "daily-total", intro: s(6, "原帖的 Mini 示例分别列出 EPA 360 mg、DHA 243 mg。可把标签上的两个成分和自己的使用数量填入下方，核对算术总量。") },
      { title: "不同包装，换算每日成本", label: "交互换算", layout: "calculator", calculator: "unit-cost", intro: s(4, "帖子出现 200 元 / 90 粒等价格信息。把实际购买价格和数量填入，可按相同口径比较花费。") },
      { title: "共识与分歧速查", label: "保留讨论边界", layout: "matrix", columns: ["原帖观点", "仍有分歧"], rows: [
        { label: "是否购买", cells: [s(12, "鱼吃够就不必买；对睡眠、记忆多无明显体感。"), s(12, "方便划算与食物替代之间如何权衡，意见不同。")] },
        { label: "如何挑选", cells: [s(12, "关注 EPA+DHA、形态及第三方检测。"), s(12, "品牌没有压倒性赢家；效果与替代其他油脂的关系也有争议。")] },
      ] },
    ];
    chapter = { blockIds: page.source.blocks.map(b => b.id), palette: "marine", category: "社群讨论 · 鱼油", subtitle: s(0, "从长期体感、食物替代到品牌参数，把讨论中的共识与分歧分开阅读。"), lead: s(0), imageId: page.assets[0].id, sections };
  } else {
    sections = [
      { title: "生啤与熟啤：处理工艺的取舍", label: "维度 · 杀菌方式", layout: "comparison", items: [
        item(3, "熟啤", "原文描述：巴氏杀菌灭活酵母与杂菌，保质期较长，同时会影响部分风味物质。", { badge: "原文工艺描述", metrics: [metric(3, "温度", "60–70 度"), metric(3, "保质期描述", "半年到一年")], takeaway: s(3, "原文将生熟的本质概括为保鲜与保质之间的取舍，无高低之分。") }),
        item(3, "生啤", "原文描述：不加热，用微孔膜或硅藻土物理过滤除菌，保留更多活性风味，保质期较短。", { badge: "原文工艺描述", metrics: [metric(3, "方法", "物理过滤"), metric(3, "保质期描述", "一周到一个月")], takeaway: s(3, "原文举例包括饭店扎啤和标注“纯生”的产品。") }),
      ] },
      { title: "同一取舍，换一种方式理解", label: "图文解释", layout: "illustrated", imageId: page.assets[1].id, items: [item(3, "保鲜与保质", "把生与熟放在处理工艺这个维度比较，才能理解原文所说的风味与保存之间的取舍。", { takeaway: s(3, "这两个词在原文中不构成品质排名。") })] },
      { title: "鲜啤：时间状态，并非工艺标签", label: "维度 · 新鲜程度", layout: "illustrated", imageId: page.assets[2].id, items: [
        item(6, "出厂两天的熟啤", "原文用这个例子说明：熟啤也可以很新鲜。", { tone: "positive", badge: "原文示例" }),
        item(6, "存放五个月的生啤", "原文用这个例子说明：生啤也可能已经不新鲜。", { tone: "caution", badge: "原文示例" }),
      ] },
      { title: "精酿：生产主体与酿造理念", label: "维度 · 酿造主体", layout: "cards", items: [
        item(9, "独立与多样性", "原文所述的国际共识侧重独立小型酒厂、非大集团控股，以及风味多样性。", { badge: "原文定义线索" }),
        item(9, "标签的使用边界", "原文认为国内缺少精酿的法定标准，提醒读者不要仅凭包装上的词判断。", { badge: "原文观点" }),
        item(9, "与另外两维分开", "原文指出精酿可以是生啤或熟啤，也可以是新酒或陈酒。", { badge: "三个维度独立" }),
      ] },
      { title: "看到包装词，先核对哪个问题", label: "交互探索", layout: "explorer", items: [
        item(3, "生 / 熟", "先看原文如何描述处理工艺。这个词回答的是处理方式问题。"),
        item(6, "鲜", "先看生产日期与保存状态。原文提醒：“鲜”描述新鲜程度，与生熟不是同一个维度。"),
        item(9, "精酿", "先看生产主体与酿造理念。原文不把这个词与是否杀菌、是否刚出厂绑定。"),
      ] },
      { title: "选购信息核对清单", label: "原文提出的三个问题", layout: "checklist", items: [
        item(11, "看保质期", "原文用保质期长短来辅助判断处理方式；具体表述及限定条件可点击引用核对。"),
        item(11, "看生产日期", "原文建议关注生产日期，挑选较近的日期。"),
        item(11, "看配料表", "原文对大米、淀粉和香精持排斥态度。这是原文的选购判断，不在重构页升级为统一行业标准。"),
      ] },
      { title: "三个维度的全景速查", layout: "matrix", columns: ["回答什么问题", "原文中的关系"], rows: [
        { label: "生 / 熟", cells: [s(3, "怎样处理和除菌？"), s(3, "保鲜与保质的取舍。")] },
        { label: "鲜", cells: [s(6, "当前是否新鲜？"), s(6, "与生熟不是同一维度。")] },
        { label: "精酿", cells: [s(9, "谁在酿、重视什么？"), s(9, "可以与生、熟、新、陈组合。")] },
      ] },
    ];
    chapter = { blockIds: page.source.blocks.map(b => b.id), palette: "amber", category: "概念解读 · 啤酒", subtitle: s(1, "把处理工艺、新鲜程度与酿造主体分开，再回到包装信息作判断。"), lead: s(1), imageId: page.assets[0].id, sections };
  }
  if (fixture.id === "beer") {
    chapter.displayTitle = page.source.title.split("：")[0];
    chapter.lead.emphasis = ["杀菌方式", "新鲜度", "酿造主体"];
    for (const [i, label] of [[0, "杀菌方式"], [2, "新鲜程度"], [3, "酿造主体"]] as const) chapter.sections[i].navLabel = label;
  } else {
    chapter.lead.emphasis = ["100–300 元", "对睡眠、记忆几乎无体感", "EPA+DHA"];
    for (const [i, label] of [[0, "服用体验"], [1, "食物与补剂"], [2, "品牌参数"]] as const) chapter.sections[i].navLabel = label;
  }
  page.design = { direction: "以用户参考页为视觉标准的可追溯专题", html: "", css: "", assets: [], composition: { version: 1, chapters: [chapter] } };
  page.designParts = [page.design];
  validateThemedReadingVersion(page);
  const files: Record<string, string> = {};
  for (const kind of ["offline", "embedded"]) { files[kind] = path.join(output, `${fixture.id}-${kind}.html`); await fs.writeFile(files[kind], themedReadingDocument(page, kind === "embedded" ? "composition-fixture" : undefined, urls)); }
  results.push({ id: fixture.id, title: page.source.title, page, files });
}
await fs.writeFile(path.join(output, "fixtures.json"), JSON.stringify({ modelCalls: 0, fixtures: results }, null, 2));
process.stdout.write(`${output}\n`);
}
void main();
