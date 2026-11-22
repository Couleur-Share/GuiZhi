package ask

import (
	"encoding/json"
	"strings"
	"testing"

	"guizhi-mygo-shell/aiflow"
	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/locales"
)

// 序列化与读回：写出的 JSON 与渲染层互通；nil 切片写成空数组；损坏的记录报错而不是当成空会话。

func TestMarshalAskMessagesNormalizesNilSlices(t *testing.T) {
	raw, err := marshalAskMessages([]services.AskMessage{{ID: "m", Question: "q", Status: services.AskStatusDone}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw, `"sources":[]`) || !strings.Contains(raw, `"steps":[]`) || strings.Contains(raw, "null") {
		t.Fatalf("渲染层把 null 当成缺失：%s", raw)
	}
	if raw, _ := marshalAskMessages(nil); raw != "[]" {
		t.Fatalf("空列表写成 []：%s", raw)
	}
}

func TestAskMessagesRoundTrip(t *testing.T) {
	in := []services.AskMessage{{
		ID: "m1", Question: "q", Answer: "a", Status: services.AskStatusDone, Model: "m", UsedFallback: true, Truncated: true,
		Sources: []services.AskSource{source(1, "i1", "fp")}, EvidenceSources: []services.AskSource{source(1, "i1", "fp")},
		Steps: []string{"s"}, Warnings: []string{"w"},
	}, {
		ID: "m2", Question: "q2", Status: services.AskStatusError, Error: "e", ErrorKind: services.AskErrorGeneric, Stopped: true,
		Sources: []services.AskSource{}, Steps: []string{},
	}}
	raw, err := marshalAskMessages(in)
	if err != nil {
		t.Fatal(err)
	}
	out, err := parseStoredMessages(raw, defaultID)
	if err != nil {
		t.Fatal(err)
	}
	again, _ := marshalAskMessages(out)
	if raw != again {
		t.Fatalf("往返应保持不变：\n%s\n%s", raw, again)
	}
}

func TestParseStoredMessagesReadsRendererFormat(t *testing.T) {
	// 渲染层写下的会话：没有 stopped 字段，已停止是 errorKind=generic + 文案
	body := `[{"id":"a","question":"q","answer":"x","sources":[],"steps":["s"],"status":"error","error":"已停止","errorKind":"generic","evidenceSources":[{"ordinal":1,"kind":"item","refId":"i","title":"t"}],"warnings":["w"],"model":"m"}]`
	msgs, err := parseStoredMessages(body, defaultID)
	if err != nil {
		t.Fatal(err)
	}
	m := msgs[0]
	if m.Error != "已停止" || m.Model != "m" || len(m.EvidenceSources) != 1 || m.Warnings[0] != "w" || ClassOf(m) != ClassGeneric {
		t.Fatalf("%+v", m)
	}
}

func TestParseStoredRunningBecomesInterruptedAndNotStopped(t *testing.T) {
	msgs, err := parseStoredMessages(`[{"id":"a","question":"q","status":"running","stopped":true}]`, defaultID)
	if err != nil {
		t.Fatal(err)
	}
	m := msgs[0]
	if m.Status != services.AskStatusError || m.Error != locales.AskSessionInterrupted() || m.Stopped || ClassOf(m) != ClassGeneric {
		t.Fatalf("没完成的轮次是「已中断」，不是「已停止」：%+v", m)
	}
}

func TestParseArticleMessages(t *testing.T) {
	good := `[{"id":"a","question":"q","answer":"x","status":"done","sources":null,"warnings":null,"webEnabled":true,"webStatus":"ready"}]`
	msgs, err := parseArticleMessages(good)
	if err != nil {
		t.Fatal(err)
	}
	if msgs[0].Sources == nil || msgs[0].Warnings == nil {
		t.Fatalf("缺省数组补齐：%+v", msgs[0])
	}
	running, err := parseArticleMessages(`[{"id":"a","question":"q","answer":"x","status":"running","step":"s"}]`)
	if err != nil {
		t.Fatal(err)
	}
	if m := running[0]; m.Status != services.AskStatusError || m.Error == nil || *m.Error != locales.AskSessionInterrupted() || m.Step != nil {
		t.Fatalf("%+v", m)
	}
	for name, bad := range map[string]string{
		"非 JSON": "nope", "不是数组": `{}`, "元素不是对象": `[1]`, "缺 id": `[{"question":"q","answer":"a"}]`,
		"缺 question": `[{"id":"i","answer":"a"}]`, "缺 answer": `[{"id":"i","question":"q"}]`, "answer 类型不对": `[{"id":"i","question":"q","answer":1}]`,
	} {
		if _, err := parseArticleMessages(bad); err == nil {
			t.Fatalf("%s 应报格式损坏", name)
		}
	}
}

func TestArticleMessagesMarshalNormalizes(t *testing.T) {
	raw, err := marshalArticleMessages([]aiflow.ArticleMessage{{ID: "i", Question: "q", Status: services.AskStatusDone}})
	if err != nil {
		t.Fatal(err)
	}
	var back []map[string]any
	if err := json.Unmarshal([]byte(raw), &back); err != nil {
		t.Fatal(err)
	}
	if _, ok := back[0]["sources"].([]any); !ok {
		t.Fatalf("sources 应是数组：%s", raw)
	}
	if _, ok := back[0]["warnings"].([]any); !ok {
		t.Fatalf("warnings 应是数组：%s", raw)
	}
}

func TestBuildArticleTitle(t *testing.T) {
	if buildArticleTitle(nil) != "" {
		t.Fatal("没有消息没有标题")
	}
	if got := buildArticleTitle([]aiflow.ArticleMessage{{Question: strings.Repeat("a", 40)}}); got != strings.Repeat("a", 30) {
		t.Fatalf("%q", got)
	}
}
