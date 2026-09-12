import { useMemo } from "react";
import { MarkdownBody } from "../library/MarkdownPreview";
import { linkifyCitations } from "./qa-citations";

/** 两种问答共用引用格式和安全 Markdown 渲染。 */
export function AnswerBody({ answer, ordinals, onCitation }: { answer: string; ordinals: number[]; onCitation: (ordinal: number) => void }) {
  const body = useMemo(() => linkifyCitations(answer, new Set(ordinals)), [answer, ordinals]);
  return <MarkdownBody content={body} onCitationClick={onCitation} />;
}
