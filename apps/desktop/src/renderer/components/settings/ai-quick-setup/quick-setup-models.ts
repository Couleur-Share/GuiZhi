import type {
  AIModelCapabilities,
  AIModelRoute,
  AIQuickSetupInput,
} from "../../../stores/settings.store";

export type QuickSetupGoal =
  | "basic"
  | "semantic"
  | "vision"
  | "audio"
  | "imageGen";

export type QuickSetupRole = "text" | "embedding" | "vision" | "audio" | "imageGen";

export const ROLE_META: Record<
  QuickSetupRole,
  { label: string; capabilities: AIModelCapabilities; routes: AIModelRoute[] }
> = {
  text: { label: "基础问答", capabilities: { chat: true }, routes: ["mainText", "fastText"] },
  embedding: { label: "语义检索", capabilities: { chat: false, embedding: true }, routes: ["embedding"] },
  vision: { label: "图片识别", capabilities: { chat: true, vision: true }, routes: ["visionText"] },
  audio: { label: "语音转写", capabilities: { chat: false, audioTranscription: true }, routes: ["audioText"] },
  imageGen: { label: "正文配图", capabilities: { chat: false, imageGeneration: true }, routes: ["imageGen"] },
};

export function rolesForGoals(goals: Set<QuickSetupGoal>): QuickSetupRole[] {
  const roles: QuickSetupRole[] = [];
  if (goals.has("basic")) roles.push("text");
  if (goals.has("semantic")) roles.push("embedding");
  if (goals.has("vision")) roles.push("vision");
  if (goals.has("audio")) roles.push("audio");
  if (goals.has("imageGen")) roles.push("imageGen");
  return roles;
}

const HINTS: Record<QuickSetupRole, RegExp[]> = {
  text: [/gpt-4/i, /claude/i, /gemini/i, /qwen/i, /deepseek/i, /llama/i],
  embedding: [/embed/i, /bge/i, /e5(?:-|$)/i, /gte/i],
  vision: [/vision/i, /\bvl\b/i, /gpt-4o/i, /gemini/i, /qwen.*vl/i],
  audio: [/whisper/i, /transcri/i, /speech.*text/i],
  imageGen: [/gpt-image/i, /dall-e/i, /imagen/i, /image.*gener/i],
};

export function recommendModel(modelIds: string[], role: QuickSetupRole): string {
  return modelIds.find((id) => HINTS[role].some((pattern) => pattern.test(id))) ?? modelIds[0] ?? "";
}

export function buildQuickSetupInput(
  provider: AIQuickSetupInput["provider"],
  roles: QuickSetupRole[],
  selections: Partial<Record<QuickSetupRole, string>>,
  verifiedRoles: Set<QuickSetupRole>,
): AIQuickSetupInput {
  const models: AIQuickSetupInput["models"] = [];
  const routes: AIQuickSetupInput["routes"] = {};
  const modelIndexes = new Map<string, number>();
  for (const role of roles) {
    const model = selections[role]?.trim();
    if (!model) continue;
    let index = modelIndexes.get(model);
    if (index === undefined) {
      index = models.length;
      modelIndexes.set(model, index);
      models.push({
        name: model,
        model,
        capabilities: { ...ROLE_META[role].capabilities },
        verified: verifiedRoles.has(role),
      });
    } else {
      models[index].capabilities = {
        ...models[index].capabilities,
        ...ROLE_META[role].capabilities,
        chat:
          models[index].capabilities.chat !== false ||
          ROLE_META[role].capabilities.chat !== false,
      };
      models[index].verified = models[index].verified && verifiedRoles.has(role);
    }
    for (const route of ROLE_META[role].routes) routes[route] = index;
  }
  return { provider, models, routes };
}
