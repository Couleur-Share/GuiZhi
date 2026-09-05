import { describe, expect, it } from "vitest";
import cases from "../../fixtures/research-cases.json";
import { analyzeResearchCandidates } from "@guizhi/shared/utils/research-analysis";
import { fallbackResearchPlan, writeResearchReport } from "../../../src/main/services/research/research-ai";
import { createEvidenceSnapshot, validateReport } from "../../../src/main/services/research/report-evidence";
import { researchFixture } from "../../helpers/research";

describe("固定中文问题、原文与报告响应", () => {
  it.each(cases)("$topic", async (sample) => {
    const detail = researchFixture(); detail.run.topic = sample.topic;
    const plan = fallbackResearchPlan(sample.topic);
    expect(plan.intent).toBe(sample.intent);
    detail.run.context = { phase: "idle", seriesId: detail.run.id, reportOutdated: false, policyVersion: "v2", plan };
    const candidate = { ...detail.candidates[0], title: sample.text, snippet: sample.text, publishedAt: sample.undated ? null : detail.run.rangeTo - 1 };
    detail.candidates = analyzeResearchCandidates(sample.topic, detail.run.rangeFrom, detail.run.rangeTo, [candidate]).candidates;
    if (sample.insufficient) { expect(() => createEvidenceSnapshot(detail)).toThrow(/足够/); return; }
    const snapshot = createEvidenceSnapshot(detail);
    const output = await writeResearchReport(snapshot.packet, new AbortController().signal, async () => `${sample.analysis} [R1]`);
    expect(() => validateReport(output, snapshot.packet)).not.toThrow();
    expect(snapshot.packet.items[0].passages?.[0].text).toBe(sample.text);
  });
});
