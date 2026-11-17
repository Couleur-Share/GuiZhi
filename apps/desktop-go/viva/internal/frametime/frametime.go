// Package frametime 是模块完成定义第 4 条（稳态帧、滚动一步的耗时）的读数工具，只给测试用。
//
// 耗时与机器、负载有关，所以读数测试默认跳过（环境变量 VIVA_FRAME_BUDGET=1 才跑），
// 结果记进开发文档 §8.4，不当成会随机失败的门禁。
package frametime

import (
	"fmt"
	"math"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/vivaui/viva/app"
)

// Stats 是一组耗时的分位数。
type Stats struct {
	N                        int
	Mean, P50, P95, P99, Max time.Duration
}

// Measure 依次调用 step 共 n 次并记录每次耗时；step 返回错误时立即返回，不给残缺读数。
func Measure(n int, step func(i int) error) (Stats, error) {
	if n <= 0 {
		return Stats{}, fmt.Errorf("次数必须为正：%d", n)
	}
	d := make([]time.Duration, 0, n)
	for i := 0; i < n; i++ {
		start := time.Now()
		if err := step(i); err != nil {
			return Stats{}, err
		}
		d = append(d, time.Since(start))
	}
	return Summarize(d), nil
}

// Summarize 求分位数（最近秩法：第 ceil(q×n) 小的值）。不改动入参。
func Summarize(d []time.Duration) Stats {
	if len(d) == 0 {
		return Stats{}
	}
	s := append([]time.Duration(nil), d...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	at := func(q float64) time.Duration {
		rank := int(math.Ceil(q*float64(len(s)))) - 1
		if rank < 0 {
			rank = 0
		}
		if rank >= len(s) {
			rank = len(s) - 1
		}
		return s[rank]
	}
	var sum time.Duration
	for _, v := range s {
		sum += v
	}
	return Stats{N: len(s), Mean: sum / time.Duration(len(s)), P50: at(0.50), P95: at(0.95), P99: at(0.99), Max: s[len(s)-1]}
}

// String 是一行可读的读数，单位毫秒。
func (s Stats) String() string {
	ms := func(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }
	return fmt.Sprintf("n=%d mean=%.2f ms p50=%.2f ms p95=%.2f ms p99=%.2f ms max=%.2f ms",
		s.N, ms(s.Mean), ms(s.P50), ms(s.P95), ms(s.P99), ms(s.Max))
}

// Fingerprint 把无障碍树压成一个字符串（按树序的「角色|名称|纵坐标」），用来核对操作确实改变了画面。
// Texts() 含被裁剪掉的文字，不能用来判断滚动有没有发生；被裁掉的节点不在无障碍树里，所以用它。
func Fingerprint(nodes []app.AccessNode) string {
	var b strings.Builder
	for _, n := range nodes {
		fmt.Fprintf(&b, "%v|%s|%.0f;", n.Role, n.Label, n.Bounds.Y)
	}
	return b.String()
}

// HeapInUse 先做一次完整 GC，再读存活的 Go 堆（字节）。比较「打开页面前后」的读数得到 Go 堆增量，
// 它只是模块占内存的一部分（不含 WebView2、显存与系统库），但不受进程工作集的随机波动影响。
func HeapInUse() uint64 {
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.HeapAlloc
}
