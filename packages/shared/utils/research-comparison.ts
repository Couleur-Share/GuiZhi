import type { ResearchRunDetail, ResearchComparison } from "../types";

export function compareResearch(current: ResearchRunDetail, baseline?: ResearchRunDetail | null): ResearchComparison {
  const result: ResearchComparison = { runId: current.run.id, baselineRunId: baseline?.run.id ?? null, warnings: [], changes: [] };
  if (!baseline) return result;
  const key = (c: ResearchRunDetail["candidates"][number]) => `${c.source}:${c.source === "web" ? c.normalizedUrl : c.externalId || c.normalizedUrl}`;
  const before = new Map(baseline.candidates.map((c) => [key(c), c]));
  const now = new Map(current.candidates.map((c) => [key(c), c]));
  const changedPlan = JSON.stringify(current.run.context?.plan?.queries) !== JSON.stringify(baseline.run.context?.plan?.queries)
    || current.run.context?.policyVersion !== baseline.run.context?.policyVersion;
  if (changedPlan) result.warnings.push("plan_changed");
  if ((current.run.timeScope ?? "recent") !== (baseline.run.timeScope ?? "recent")) result.warnings.push("time_scope_changed");
  const comparable = (source: string) => [current, baseline].every((d) => d.sources.find((s) => s.source === source)?.status === "succeeded"
    && !(d.attempts ?? []).some((a) => a.source === source && (a.capped || a.errorCode)) && Boolean(d.attempts?.some((a) => a.source === source)));
  if (current.run.sources.some((s) => !comparable(s))) result.warnings.push("coverage_incomparable");
  for (const [identity, candidate] of now) {
    const previous = before.get(identity);
    if (!previous) { result.changes.push({ kind: "new", current: candidate }); continue; }
    if (current.run.timeScope !== "all" && previous.dateConfidence !== "low" && previous.publishedAt != null && previous.publishedAt < current.run.rangeFrom) { result.changes.push({ kind: "outside_window", current: candidate, previous }); continue; }
    const oldDoc = baseline.documents?.find((d) => d.candidateId === previous.id);
    const newDoc = current.documents?.find((d) => d.candidateId === candidate.id);
    const changed = oldDoc?.contentHash && newDoc?.contentHash && oldDoc.contentHash !== newDoc.contentHash;
    const engagementChanges: Record<string, number> = {};
    for (const [metric, value] of Object.entries(candidate.engagement)) {
      const old = previous.engagement[metric as keyof typeof previous.engagement];
      if (typeof value === "number" && Number.isFinite(value) && typeof old === "number" && Number.isFinite(old)) engagementChanges[metric] = value - old;
    }
    result.changes.push({ kind: changed ? "changed" : "continued", current: candidate, previous, engagementChanges });
  }
  for (const [identity, previous] of before) {
    if (now.has(identity)) continue;
    const outside = current.run.timeScope !== "all" && previous.dateConfidence !== "low" && previous.publishedAt != null && previous.publishedAt < current.run.rangeFrom;
    result.changes.push({ kind: outside ? "outside_window" : changedPlan || !comparable(previous.source) || (current.run.timeScope !== "all" && (previous.publishedAt == null || previous.dateConfidence === "low" || previous.publishedAt > current.run.rangeTo)) ? "unknown" : "not_found", previous });
  }
  return result;
}
