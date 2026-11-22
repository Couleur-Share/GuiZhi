package ask

import (
	"slices"

	"guizhi-mygo-shell/aiflow"
	"guizhi-mygo-shell/native/services"
)

// stripTarget 只留下定位所需的最小字段（条目与视图）：证据被清除的条目，选段与版本信息也不能再留在内存里。
func stripTarget(t aiflow.ArticleTarget) *aiflow.ArticleTarget {
	return &aiflow.ArticleTarget{ItemID: t.ItemID, View: t.View}
}

// sanitizeTarget 对应 sanitizeCachedTarget：条目的证据已被清除时脱敏，否则原样返回；nil 原样返回。
func sanitizeTarget(ev *Evidence, t *aiflow.ArticleTarget) *aiflow.ArticleTarget {
	if t == nil || !ev.Has(t.ItemID) {
		return t
	}
	return stripTarget(*t)
}

// cloneTarget 拷贝一份目标：状态里存的目标不与调用方共享指针。
func cloneTarget(t aiflow.ArticleTarget) *aiflow.ArticleTarget {
	out := t
	out.SourceKind = copyOf(t.SourceKind)
	out.VersionID = copyOf(t.VersionID)
	out.Selection = copyOf(t.Selection)
	out.Floor = copyOf(t.Floor)
	return &out
}

func copyOf[T any](p *T) *T {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

// evidenceKey 是证据的去重键（`kind:refId:fingerprint`，没有快照时指纹为空）。
func evidenceKey(s services.AskSource) string {
	fp := ""
	if s.Evidence != nil {
		fp = s.Evidence.Fingerprint
	}
	return s.Kind + ":" + s.RefID + ":" + fp
}

// mergeEvidence 合并两批来源，按键去重：同键后来者覆盖内容、位置保持先到者（JS Map 的语义）。
func mergeEvidence(previous, next []services.AskSource) []services.AskSource {
	out := make([]services.AskSource, 0, len(previous)+len(next))
	index := map[string]int{}
	for _, batch := range [][]services.AskSource{previous, next} {
		for _, s := range batch {
			key := evidenceKey(s)
			if at, ok := index[key]; ok {
				out[at] = s
				continue
			}
			index[key] = len(out)
			out = append(out, s)
		}
	}
	return out
}

// appendUnique 在列表末尾追加一条，已有则原样返回（返回新切片，不改动入参）。
func appendUnique(list []string, item string) []string {
	if slices.Contains(list, item) {
		return list
	}
	return append(slices.Clone(list), item)
}
