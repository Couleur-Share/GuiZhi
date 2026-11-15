package aiflow

// Embedding 请求（对应 services/knowledge-ai/embeddings.ts 与 packages/shared/utils/embedding-values.ts）。

import (
	"context"
	"errors"
	"fmt"
	"math"
)

const embeddingsTimeoutMs = 60_000

// L2Normalize 对应 l2Normalize：范数非有限或为 0 时返回同长度的全零向量。
func L2Normalize(values []float64) []float64 {
	var sumOfSquares float64
	for _, v := range values {
		sumOfSquares += v * v
	}
	norm := math.Sqrt(sumOfSquares)
	out := make([]float64, len(values))
	if math.IsNaN(norm) || math.IsInf(norm, 0) || norm == 0 {
		return out
	}
	for i, v := range values {
		out[i] = v / norm
	}
	return out
}

// ParseEmbeddingsResponse 对应 parseEmbeddingsResponse：按 index 对齐输入顺序，校验数量、连续性与维度。
func ParseEmbeddingsResponse(body string, expectedCount int) ([][]float64, error) {
	parsed, err := parseJSON(body)
	if err != nil {
		return nil, errors.New("Embeddings 响应不是合法 JSON")
	}
	root, _ := parsed.(map[string]any)
	data, isArray := root["data"].([]any)
	if !isArray || len(data) != expectedCount {
		actual := 0
		if isArray {
			actual = len(data)
		}
		return nil, fmt.Errorf("Embeddings 响应数量不匹配：期望 %d，实际 %d", expectedCount, actual)
	}

	vectors := make([][]float64, expectedCount)
	filled := make([]bool, expectedCount)
	for position, raw := range data {
		entry, _ := raw.(map[string]any)
		embedding, ok := entry["embedding"].([]any)
		if !ok || len(embedding) == 0 {
			return nil, errors.New("Embeddings 响应缺少向量数据")
		}
		index := position
		if n, isNumber := entry["index"].(float64); isNumber && n >= 0 && n < float64(expectedCount) {
			index = int(n)
			if float64(index) != n {
				// 小数下标：JS 里 vectors[1.5] 写的是对象属性而不是数组元素，该位置仍是空洞；这里同样不落位
				continue
			}
		}
		vector := make([]float64, len(embedding))
		for i, v := range embedding {
			f, ok := jsNumber(v)
			if !ok {
				f = math.NaN()
			}
			vector[i] = f
		}
		vectors[index] = vector
		filled[index] = true
	}
	// TS 版用 vectors.some(v => !v) 做连续性检查，但 some 会跳过空洞，重复 index 造成的缺口检不出来，
	// 最终把带空洞的数组交给调用方。Go 版按注释的本意处理：有缺口就报「索引不连续」。
	for _, ok := range filled {
		if !ok {
			return nil, errors.New("Embeddings 响应索引不连续")
		}
	}
	dims := len(vectors[0])
	for _, v := range vectors {
		if len(v) != dims {
			return nil, errors.New("Embeddings 响应向量维度不一致")
		}
	}
	return vectors, nil
}

// readPromptTokens 取 embeddings 响应里的 usage.prompt_tokens；provider 不回报时按 0 计。
func readPromptTokens(body string) float64 {
	parsed, err := parseJSON(body)
	if err != nil {
		return 0
	}
	root, _ := parsed.(map[string]any)
	usage, _ := root["usage"].(map[string]any)
	n := numberOrZero(usage["prompt_tokens"])
	if math.IsInf(n, 0) {
		return 0 // TS 会把 Infinity 原样交给统计，序列化后变成 null；这里直接按「不回报」计
	}
	return math.Max(0, math.Trunc(n))
}

// EmbedTexts 对应 embedTexts：批量嵌入文本，返回 L2 归一化后的向量（与输入同序）。
func (r *Runner) EmbedTexts(ctx context.Context, cfg AIConfig, texts []string) ([][]float64, error) {
	if len(texts) == 0 {
		return [][]float64{}, nil
	}
	protocol := ResolveAIProtocol(cfg.APIProtocol, cfg.Provider, cfg.APIURL)
	endpoint := BuildEmbeddingsEndpoint(ResolveProtocolBase(cfg.APIURL, protocol))
	if endpoint == "" {
		return nil, fmt.Errorf("当前协议不支持 embeddings 接口: %s", protocol)
	}

	body := newObj().set("model", cfg.Model).set("input", texts).String()
	resp, err := cancellable(ctx, func() (TransportResponse, error) {
		return r.Client.Transport.Request(ctx, TransportRequest{
			Method: "POST", URL: endpoint, Headers: BuildHeaders(protocol, cfg.APIKey, HeaderOptions{}),
			Body: body, TimeoutMs: embeddingsTimeoutMs,
		})
	})
	if err != nil {
		r.recordUsage(UsageEntry{Scenario: "embedding", Model: cfg.Model, Failed: true})
		return nil, err
	}
	if !resp.OK {
		r.recordUsage(UsageEntry{Scenario: "embedding", Model: cfg.Model, Failed: true})
		detail := resp.Body
		if resp.Error != nil && *resp.Error != "" { // `response.error || response.body`：空串落到 body
			detail = *resp.Error
		}
		return nil, fmt.Errorf("Embeddings 请求失败 (HTTP %d): %s", resp.Status, u16Head(detail, 300))
	}

	// 一次全库索引会打成百上千次 embeddings，这些消耗必须进用量面板
	r.recordUsage(UsageEntry{Scenario: "embedding", Model: cfg.Model, PromptTokens: readPromptTokens(resp.Body)})

	vectors, err := ParseEmbeddingsResponse(resp.Body, len(texts))
	if err != nil {
		return nil, err
	}
	for i := range vectors {
		vectors[i] = L2Normalize(vectors[i])
	}
	return vectors, nil
}
