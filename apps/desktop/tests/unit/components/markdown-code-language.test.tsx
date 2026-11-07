import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { MarkdownBody } from "../../../src/renderer/components/library/MarkdownPreview";

const render = (markdown: string) => renderToStaticMarkup(<MarkdownBody content={markdown} />);
const fenced = (language: string, code: string) => `\`\`\`${language}\n${code}\n\`\`\``;

describe("代码块语言名", () => {
  it("未注册的语言按纯代码显示，不让整块内容渲染失败", () => {
    const html = render(fenced("powershell", "Get-ChildItem | Select-Object Name"));
    expect(html).toContain("Get-ChildItem | Select-Object Name");
    expect(html).not.toContain("hljs-");
  });

  it("网页常见的语言别名接到已注册的语言并高亮", () => {
    for (const language of ["python3", "py3"]) {
      expect(render(fenced(language, 'def greet():\n    return "hi"'))).toContain('class="hljs-keyword"');
    }
    expect(render(fenced("markup", '<div class="note">hi</div>'))).toContain('class="hljs-tag"');
    for (const language of ["zsh", "shell-session"]) {
      expect(render(fenced(language, '$ echo "hi"'))).toContain("hljs-");
    }
  });
});
