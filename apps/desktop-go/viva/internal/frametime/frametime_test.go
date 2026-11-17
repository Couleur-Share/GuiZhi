package frametime

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
)

func TestSummarizeNearestRank(t *testing.T) {
	// 1…100 ms：最近秩法下 p50 = 第 50 小 = 50 ms，p95 = 95 ms，p99 = 99 ms，max = 100 ms。
	d := make([]time.Duration, 100)
	for i := range d {
		d[i] = time.Duration(i+1) * time.Millisecond
	}
	got := Summarize(d)
	want := Stats{N: 100, Mean: 50500 * time.Microsecond, P50: 50 * time.Millisecond, P95: 95 * time.Millisecond, P99: 99 * time.Millisecond, Max: 100 * time.Millisecond}
	if got != want {
		t.Fatalf("分位数不对：得到 %+v，期望 %+v", got, want)
	}
}

func TestSummarizeSmallSamples(t *testing.T) {
	// 只有一个样本时所有分位数都是它；两个样本时 p50 取较小的那个（第 ceil(0.5×2) = 1 小）。
	one := Summarize([]time.Duration{7 * time.Millisecond})
	if one.P50 != 7*time.Millisecond || one.Max != 7*time.Millisecond || one.Mean != 7*time.Millisecond || one.N != 1 {
		t.Fatalf("单样本读数不对：%+v", one)
	}
	two := Summarize([]time.Duration{9 * time.Millisecond, 3 * time.Millisecond})
	if two.P50 != 3*time.Millisecond || two.P95 != 9*time.Millisecond || two.Max != 9*time.Millisecond || two.Mean != 6*time.Millisecond {
		t.Fatalf("双样本读数不对：%+v", two)
	}
}

func TestSummarizeEmptyAndInputUntouched(t *testing.T) {
	if got := Summarize(nil); got != (Stats{}) {
		t.Fatalf("空输入应当是零值：%+v", got)
	}
	in := []time.Duration{3, 1, 2}
	Summarize(in)
	if in[0] != 3 || in[1] != 1 || in[2] != 2 {
		t.Fatalf("Summarize 不能改动入参的顺序：%v", in)
	}
}

func TestMeasureCountsEveryStep(t *testing.T) {
	calls := 0
	got, err := Measure(25, func(i int) error {
		if i != calls {
			t.Fatalf("第 %d 次调用收到的序号是 %d", calls, i)
		}
		calls++
		return nil
	})
	if err != nil || calls != 25 || got.N != 25 {
		t.Fatalf("应当调用 25 次并给出 25 个样本：calls=%d N=%d err=%v", calls, got.N, err)
	}
}

func TestMeasureStopsOnError(t *testing.T) {
	boom := errors.New("出帧失败")
	calls := 0
	got, err := Measure(10, func(int) error {
		calls++
		if calls == 3 {
			return boom
		}
		return nil
	})
	if !errors.Is(err, boom) || calls != 3 || got != (Stats{}) {
		t.Fatalf("出错应当立即停下且不给残缺读数：calls=%d got=%+v err=%v", calls, got, err)
	}
	if _, err := Measure(0, func(int) error { return nil }); err == nil {
		t.Fatalf("次数为 0 应当报错，而不是返回空读数")
	}
}

func TestStatsStringHasUnits(t *testing.T) {
	s := Stats{N: 3, Mean: 1250 * time.Microsecond, P50: 1500 * time.Microsecond, P95: 2 * time.Millisecond, P99: 2 * time.Millisecond, Max: 4 * time.Millisecond}.String()
	for _, want := range []string{"n=3", "mean=1.25 ms", "p50=1.50 ms", "p95=2.00 ms", "max=4.00 ms"} {
		if !strings.Contains(s, want) {
			t.Fatalf("读数行缺少 %q：%s", want, s)
		}
	}
}
func TestFingerprintSeesScrollAndOrder(t *testing.T) {
	node := func(label string, y float32) app.AccessNode {
		return app.AccessNode{Role: gfx.RoleButton, Label: label, Bounds: gfx.Rect{Y: y}}
	}
	base := []app.AccessNode{node("a", 10), node("b", 50)}
	if Fingerprint(base) != Fingerprint([]app.AccessNode{node("a", 10), node("b", 50)}) {
		t.Fatalf("相同的树指纹必须相同")
	}
	if Fingerprint(base) == Fingerprint([]app.AccessNode{node("a", -30), node("b", 10)}) {
		t.Fatalf("内容滚动之后（纵坐标变了）指纹必须不同")
	}
	if Fingerprint(base) == Fingerprint([]app.AccessNode{node("b", 10), node("a", 50)}) {
		t.Fatalf("同样的节点换了顺序，指纹必须不同")
	}
	if Fingerprint(base) == Fingerprint(base[:1]) {
		t.Fatalf("节点被滚出视口（少了一个）指纹必须不同")
	}
	if Fingerprint(nil) != "" {
		t.Fatalf("空树的指纹应当是空串")
	}
}

var heapSink []byte

func TestHeapInUseTracksLiveAllocations(t *testing.T) {
	const size = 8 << 20
	base := HeapInUse()
	heapSink = make([]byte, size)
	for i := range heapSink {
		heapSink[i] = byte(i) // 写满，避免被优化掉
	}
	held := HeapInUse()
	if held < base+size-(1<<20) {
		t.Fatalf("持有 8 MiB 之后存活堆应至少多 7 MiB：之前 %d，之后 %d", base, held)
	}
	heapSink = nil
	freed := HeapInUse()
	if freed+size-(2<<20) > held {
		t.Fatalf("释放之后（先 GC）存活堆应当回落约 8 MiB：持有时 %d，释放后 %d", held, freed)
	}
}
