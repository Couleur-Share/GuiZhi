import type { ResearchEvidencePacket } from "@guizhi/shared/types";
import { runScenarioChat } from "./ai-invoke";

export const RESEARCH_REPORT_PROMPT_VERSION = "research-report-v1";

function evidenceText(packet: ResearchEvidencePacket): string {
  return packet.items.map((item) => [
    `[${item.ref}]`,
    `来源=${item.source}`,
    `标题=${item.title}`,
    `作者=${item.author || "未知"}`,
    `发布时间=${item.publishedAt ? new Date(item.publishedAt).toISOString() : "未知"}`,
    `日期置信度=${item.dateConfidence}`,
    `总分=${item.overallScore}`,
    `互动=${JSON.stringify(item.engagement)}`,
    `摘要=${item.snippet || "（无）"}`,
  ].join("\n")).join("\n\n");
}

export async function generateResearchReport(
  packet: ResearchEvidencePacket,
  signal?: AbortSignal,
): Promise<string> {
  const coverage = packet.sourceRuns.map((source) => `${source.source}: ${source.status}, ${source.collectedCount} 条${source.error ? `, 诊断=${source.error}` : ""}`).join("\n");
  const result = await runScenarioChat("research", [
    {
      role: "system",
      content: [
        "你是本地知识库的研究分析助手。只能依据用户给出的候选元数据总结，不能假装读过正文或视频。",
        "所有事实性结论都必须紧邻引用一个或多个已给出的编号，例如 [R1]。",
        "只能使用证据包里存在的编号；不要输出 URL、不要自行改写日期或互动数字。",
        "固定输出五个二级标题：范围与来源覆盖、主要结论、跨平台热点、平台差异、弱信号与限制。",
        "若证据薄弱或日期未知，必须明确标注，不得补充外部知识。",
      ].join("\n"),
    },
    {
      role: "user",
      content: `主题：${packet.topic}\n范围：${new Date(packet.rangeFrom).toISOString()} 至 ${new Date(packet.rangeTo).toISOString()}\n\n来源状态：\n${coverage}\n\n候选证据：\n${evidenceText(packet)}`,
    },
  ], { temperature: 0.2, maxTokens: 3000, signal });
  return result.content.trim();
}
