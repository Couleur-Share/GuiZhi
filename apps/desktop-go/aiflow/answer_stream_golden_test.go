package aiflow

// 回答流抽取的 golden 对拍：夹具来自 apps/desktop/tests/unit/knowledge-ai/aiflow-golden-answer-stream.test.ts
// （真实 TS 的 pushAnswerChunk 按分块逐次喂入，导出每一块之后的输出）。
//
// 标了 lone 的用例里，TS 的输出会出现孤立代理项，导出时已换成 U+FFFD（Go 的 string 表示不了孤立代理项，
// 解码落成 U+FFFD，见 native-ui-development.md §8.4），这里直接比较。

import "testing"

func TestAnswerStreamGolden(t *testing.T) {
	var golden struct {
		Cases []struct {
			Name   string     `json:"name"`
			Chunks []textSpec `json:"chunks"`
			Lone   bool       `json:"lone"`
			Expect struct {
				Outputs []any `json:"outputs"`
			} `json:"expect"`
		} `json:"cases"`
	}
	loadGolden(t, "answer-stream", &golden)
	if len(golden.Cases) == 0 {
		t.Fatal("golden 里没有用例")
	}

	for _, c := range golden.Cases {
		t.Run(c.Name, func(t *testing.T) {
			stream := NewAnswerStream()
			got := make([]any, 0, len(c.Chunks))
			for _, chunk := range c.Chunks {
				got = append(got, compactText(stream.Push(chunk.String())))
			}
			assertSame(t, "每块之后的输出", c.Expect.Outputs, got)
		})
	}
}

// 零值可用：不经 NewAnswerStream 也能用。
func TestAnswerStreamZeroValue(t *testing.T) {
	var stream AnswerStream
	if got := stream.Push(`{"action":"answer","text":"你好`); got != "你好" {
		t.Fatalf("零值使用：期望「你好」，得到 %q", got)
	}
}
