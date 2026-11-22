package ask

import (
	"strings"
	"testing"
)

// split(/(`[^`]*`)/g) 之后「以反引号开头的段都按代码段处理」，包括没有闭合的孤立反引号起头的那一段；
// 中间夹一个孤立反引号的行，整行是一段（不以反引号开头），照常改写。
func TestLoneBacktickSegmentIsTreatedAsCode(t *testing.T) {
	if got := LinkifyCitations("`起头没闭合 [1]", validOrdinals, nil); got != "`起头没闭合 [1]" {
		t.Fatalf("孤立反引号起头的段不改写：%q", got)
	}
	// "`a`" 闭合后剩下的 "`b [1]" 以反引号开头，同样跳过。
	if got := LinkifyCitations("`a``b [1]", validOrdinals, nil); got != "`a``b [1]" {
		t.Fatalf("闭合的代码段之后又以反引号起头的段不改写：%q", got)
	}
	got := LinkifyCitations("前文 [1] 然后 ` 孤立 [2]", validOrdinals, nil)
	if strings.Count(got, CiteHrefPrefix) != 2 {
		t.Fatalf("行中间的孤立反引号不构成代码段，两处引用都应改写：%q", got)
	}
}
