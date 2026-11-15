package aiflow

// 语义索引（对应 services/knowledge-ai/semantic-chunk.ts 与 semantic-index.ts）：
// 拉取待索引批次 → 分块 → 嵌入 → 落库。embedding 模型未配置时静默跳过（语义检索是可选增强）。

import (
	"context"
	"strings"
	"unicode/utf16"
)

const (
	// SemanticChunkSize / SemanticChunkOverlap 是滑动窗口的窗宽与重叠，单位 UTF-16 码元（与 JS 的 string.length 一致）。
	SemanticChunkSize    = 700
	SemanticChunkOverlap = 80
	// SemanticMaxChunks 超长内容截断到前 MAX 块（约 1.5 万字），避免单条目吃掉嵌入预算。
	SemanticMaxChunks = 24

	pendingBatchSize = 10
	// maxConsecutiveFailures 连续失败即中止本轮（API 故障时避免空转烧配额）。
	maxConsecutiveFailures = 3

	// semanticNotAppliedMessage 是落库返回 false（资料已改或已删）时给界面的原因。
	semanticNotAppliedMessage = "向量未保存：资料可能已修改或删除，请重新索引最新内容"
)

// BuildEmbeddingChunks 对应 buildEmbeddingChunks：滑动窗口 + 重叠，每块前缀标题提供上下文。
// transcript 为 nil 表示没有口播转写稿。
func BuildEmbeddingChunks(title, content string, transcript *string) []string {
	parts := []string{jsTrim(content)}
	if transcript != nil {
		if trimmed := jsTrim(*transcript); trimmed != "" {
			parts = append(parts, "【口播转写稿】\n"+trimmed)
		}
	}
	nonEmpty := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" { // filter(Boolean)
			nonEmpty = append(nonEmpty, part)
		}
	}
	body := strings.Join(nonEmpty, "\n")
	trimmedTitle := jsTrim(title)
	if body == "" {
		// 只有标题的条目：标题本身就是可嵌入文本
		if trimmedTitle != "" {
			return []string{trimmedTitle}
		}
		return []string{}
	}

	prefix := ""
	if trimmedTitle != "" {
		prefix = "《" + trimmedTitle + "》\n"
	}
	units := utf16.Encode([]rune(body))
	if len(units) <= SemanticChunkSize {
		return []string{prefix + body}
	}

	chunks := []string{}
	step := SemanticChunkSize - SemanticChunkOverlap
	for start := 0; start < len(units) && len(chunks) < SemanticMaxChunks; start += step {
		end := start + SemanticChunkSize
		if end > len(units) {
			end = len(units)
		}
		// 窗口切在代理对中间时，TS 得到孤立代理项、这里得到 U+FFFD（已知差异，见 native-ui-development.md §8.4）
		chunks = append(chunks, prefix+string(utf16.Decode(units[start:end])))
		if start+SemanticChunkSize >= len(units) {
			break
		}
	}
	return chunks
}

// PendingSemanticItem 对应 PendingSemanticItem：待索引条目，内容随批下发。
type PendingSemanticItem struct {
	ID    string
	Title string
	// Content 为正文；Transcript 为 nil 表示没有口播转写稿。
	Content     string
	Transcript  *string
	ContentHash string
}

// SemanticChunkInput 对应 SemanticChunkInput：分块文本与已 L2 归一化的向量。
type SemanticChunkInput struct {
	Text   string
	Vector []float64
}

// ApplySemanticEmbeddingsInput 对应 ApplySemanticEmbeddingsInput。
type ApplySemanticEmbeddingsInput struct {
	ItemID      string
	ContentHash string
	Model       string
	Dims        int
	Chunks      []SemanticChunkInput
}

// SemanticIndexStore 是索引循环依赖的存储面（对应 window.api.semantic.listPending / applyEmbeddings）。
// 生产实现在主包 native_aiflow.go，直接调 semantic:* 的 Go 处理器。
type SemanticIndexStore interface {
	// ListPending 取一批待索引条目；contentHash 由存储计算下发，写回时原样回传。
	ListPending(ctx context.Context, model string, limit int) ([]PendingSemanticItem, error)
	// ApplyEmbeddings 落库；返回 false 表示资料已改或已删，向量未保存。
	ApplyEmbeddings(ctx context.Context, in ApplySemanticEmbeddingsInput) (bool, error)
}

// SemanticIndexRunResult 对应 SemanticIndexRunResult。
type SemanticIndexRunResult struct {
	// Skipped 为 true 表示未配置 embedding 模型，本轮未执行。
	Skipped bool
	Indexed int
	Failed  int
	// LastError 是最后一次失败的原因，空串表示本轮没有失败。
	LastError string
}

// RunSemanticIndexing 对应 runSemanticIndexing。
//
// 取消走 ctx：与 TS 一致，取消只是让循环在下一个检查点退出（不抛错），已完成的条目照常计入。
// listPending / applyEmbeddings 在 TS 里是不可取消的 IPC 调用，这里同样用 WithoutCancel 让它们跑完，
// 否则「落库已成功、却因取消被记成失败」会让计数与 TS 不一致。
func (r *Runner) RunSemanticIndexing(ctx context.Context, store SemanticIndexStore, onProgress func(indexed int)) (SemanticIndexRunResult, error) {
	cfg, ok := r.Config.AISettings().ResolveEmbeddingConfig()
	if !ok {
		return SemanticIndexRunResult{Skipped: true}, nil
	}

	var res SemanticIndexRunResult
	consecutiveFailures := 0
	attempted := map[string]bool{}
	ipcCtx := context.WithoutCancel(ctx)

	for {
		if ctx.Err() != nil {
			break
		}
		batch, err := store.ListPending(ipcCtx, cfg.Model, pendingBatchSize)
		if err != nil {
			return SemanticIndexRunResult{}, err
		}
		// 全部条目都已尝试过仍然 pending（落库失败等），停止避免死循环
		fresh := make([]PendingSemanticItem, 0, len(batch))
		for _, item := range batch {
			if !attempted[item.ID] {
				fresh = append(fresh, item)
			}
		}
		if len(fresh) == 0 {
			break
		}

		for _, item := range fresh {
			if ctx.Err() != nil {
				break
			}
			attempted[item.ID] = true
			chunks := BuildEmbeddingChunks(item.Title, item.Content, item.Transcript)
			if len(chunks) == 0 {
				continue
			}
			applied, err := r.embedAndApply(ctx, ipcCtx, store, cfg, item, chunks)
			if err != nil {
				res.Failed++
				consecutiveFailures++
				res.LastError = err.Error()
				r.logger().Warnf("[semantic] 条目嵌入失败（%s）: %s", item.ID, res.LastError)
				if consecutiveFailures >= maxConsecutiveFailures {
					return res, nil
				}
				continue
			}
			if applied {
				res.Indexed++
				consecutiveFailures = 0
				if onProgress != nil {
					onProgress(res.Indexed)
				}
			} else {
				res.Failed++
				res.LastError = semanticNotAppliedMessage
			}
		}
	}
	return res, nil
}

// embedAndApply 是 try 块的内容：嵌入 → 落库；任何一步失败都按「条目失败」处理（由调用方计数）。
func (r *Runner) embedAndApply(ctx, ipcCtx context.Context, store SemanticIndexStore, cfg AIConfig, item PendingSemanticItem, chunks []string) (bool, error) {
	vectors, err := r.EmbedTexts(ctx, cfg, chunks)
	if err != nil {
		return false, err
	}
	in := ApplySemanticEmbeddingsInput{
		ItemID:      item.ID,
		ContentHash: item.ContentHash,
		Model:       cfg.Model,
		Dims:        len(vectors[0]),
		Chunks:      make([]SemanticChunkInput, len(chunks)),
	}
	for i, text := range chunks {
		in.Chunks[i] = SemanticChunkInput{Text: text, Vector: vectors[i]}
	}
	return store.ApplyEmbeddings(ipcCtx, in)
}
