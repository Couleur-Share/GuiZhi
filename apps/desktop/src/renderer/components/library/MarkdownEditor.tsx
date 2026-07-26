import { useEffect, useRef } from "react";
import { EditorView, keymap, lineNumbers, placeholder } from "@codemirror/view";
import { EditorState, Compartment, Transaction } from "@codemirror/state";
import { markdown } from "@codemirror/lang-markdown";
import {
  defaultKeymap,
  history,
  historyKeymap,
  indentWithTab,
} from "@codemirror/commands";

interface MarkdownEditorProps {
  /** 当前条目 id：变化时以 value 重建文档（区分外部切换与用户输入） */
  docId: string;
  value: string;
  onChange: (value: string) => void;
  showLineNumbers: boolean;
  placeholderText?: string;
}

const editorTheme = EditorView.theme({
  "&": {
    height: "100%",
    fontSize: "14px",
    backgroundColor: "transparent",
  },
  ".cm-content": {
    fontFamily:
      "ui-monospace, SFMono-Regular, Menlo, Consolas, 'Liberation Mono', monospace",
    padding: "16px",
    caretColor: "hsl(var(--primary))",
  },
  ".cm-scroller": { overflow: "auto" },
  "&.cm-focused": { outline: "none" },
  ".cm-gutters": {
    backgroundColor: "transparent",
    color: "hsl(var(--muted-foreground) / 0.6)",
    border: "none",
  },
  ".cm-activeLine": { backgroundColor: "hsl(var(--muted) / 0.35)" },
  ".cm-activeLineGutter": { backgroundColor: "transparent" },
  ".cm-selectionBackground, &.cm-focused .cm-selectionBackground": {
    backgroundColor: "hsl(var(--primary) / 0.18) !important",
  },
  ".cm-cursor": { borderLeftColor: "hsl(var(--foreground))" },
});

/**
 * CodeMirror Markdown 编辑器。文档由编辑器持有；
 * 外部 value 与文档不一致时（切换条目 / AI 写回）重建文档，
 * 用户输入回流的 value 与文档一致，不会触发重建。
 */
export function MarkdownEditor({
  docId,
  value,
  onChange,
  showLineNumbers,
  placeholderText,
}: MarkdownEditorProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const viewRef = useRef<EditorView | null>(null);
  const onChangeRef = useRef(onChange);
  const lineNumbersCompartment = useRef(new Compartment());

  onChangeRef.current = onChange;

  useEffect(() => {
    if (!containerRef.current) {
      return;
    }

    const state = EditorState.create({
      doc: value,
      extensions: [
        history(),
        keymap.of([...defaultKeymap, ...historyKeymap, indentWithTab]),
        markdown(),
        EditorView.lineWrapping,
        editorTheme,
        placeholder(placeholderText ?? ""),
        lineNumbersCompartment.current.of(
          showLineNumbers ? lineNumbers() : [],
        ),
        EditorView.updateListener.of((update) => {
          if (update.docChanged) {
            onChangeRef.current(update.state.doc.toString());
          }
        }),
      ],
    });

    const view = new EditorView({ state, parent: containerRef.current });
    viewRef.current = view;

    return () => {
      view.destroy();
      viewRef.current = null;
    };
    // 仅在挂载时创建；外部内容同步与配置变化由下面的 effect 处理
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // 外部内容同步：切换条目或外部写回（AI 总结/转写等 applyServerItem）时重建文档。
  // 用户输入经 onChange 回流的 value 与文档一致，不会走到 dispatch。
  useEffect(() => {
    const view = viewRef.current;
    if (!view) {
      return;
    }
    const current = view.state.doc.toString();
    if (current !== value) {
      view.dispatch({
        changes: { from: 0, to: current.length, insert: value },
        // 这次替换不能进撤销栈：编辑器跨条目常驻（关闭「Markdown 渲染」时），
        // 切到条目 B 后按 Ctrl+Z 会把文档回滚成 A 的正文，
        // updateListener 随即把 A 的内容当作用户输入保存进 B。
        annotations: Transaction.addToHistory.of(false),
      });
    }
  }, [docId, value]);

  // 行号设置即时生效
  useEffect(() => {
    viewRef.current?.dispatch({
      effects: lineNumbersCompartment.current.reconfigure(
        showLineNumbers ? lineNumbers() : [],
      ),
    });
  }, [showLineNumbers]);

  return <div ref={containerRef} className="h-full min-h-0" />;
}
