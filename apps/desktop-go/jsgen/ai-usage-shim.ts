import { hostSync } from "./pipeline-host";
export function recordMainAiUsage(input: { scenario: string; model: string; promptTokens?: number; completionTokens?: number; failed?: boolean }) {
  hostSync("usage", input);
}
