import { useMemo } from "react";
import { MarkdownBody } from "../library/MarkdownPreview";
import { linkifyCitations, type CitationLabels } from "./qa-citations";

/** 两种问答共用引用格式和安全 Markdown 渲染。 */
export function AnswerBody({ answer, ordinals, labels, onCitation }: { answer: string; ordinals: number[]; labels?: CitationLabels; onCitation: (ordinal: number) => void }) {
  const body = useMemo(() => linkifyCitations(answer, new Set(ordinals), labels), [answer, ordinals, labels]);
  return <MarkdownBody content={body} onCitationClick={onCitation} />;
}
