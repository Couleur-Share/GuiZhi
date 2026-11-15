package kit

import (
	"bytes"
	"errors"
	"log"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestMemStoreRoundTripAndDirtyData(t *testing.T) {
	var s MemStore
	if _, ok := s.Get("a"); ok {
		t.Fatal("没保存过的键应返回 false")
	}
	if err := s.Set("a", map[string]int{"x": 1}); err != nil {
		t.Fatal(err)
	}
	raw, ok := s.Get("a")
	if !ok || string(raw) != `{"x":1}` {
		t.Fatalf("Get = %s, %v", raw, ok)
	}
	if s.Writes() != 1 {
		t.Errorf("Writes = %d", s.Writes())
	}
	s.Put("dirty", `not json at all`)
	if raw, _ := s.Get("dirty"); string(raw) != `not json at all` {
		t.Error("Put 应原样放入脏数据")
	}
	if s.Writes() != 1 {
		t.Error("Put 不计入 Writes")
	}

	s.FailSet = errors.New("磁盘已满")
	if err := s.Set("b", 1); err == nil {
		t.Error("FailSet 应让 Set 失败")
	}
	if _, ok := s.Get("b"); ok {
		t.Error("失败的 Set 不应保存")
	}
	if err := s.Set("c", make(chan int)); err == nil {
		s.FailSet = nil
		t.Error("不能编码成 JSON 的值应返回错误")
	}
}

func TestMemStoreIsConcurrencySafe(t *testing.T) {
	var s MemStore
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				_ = s.Set("k", i*1000+j)
				s.Get("k")
				s.Writes()
			}
		}(i)
	}
	wg.Wait()
	if s.Writes() != 1600 {
		t.Errorf("Writes = %d", s.Writes())
	}
}

func fastSaver(onError func(error)) *saver {
	return &saver{delay: 20 * time.Millisecond, onError: onError}
}

func TestSaverDebouncesToTheLastValue(t *testing.T) {
	s := fastSaver(nil)
	var mu sync.Mutex
	var wrote []int
	for i := 1; i <= 5; i++ {
		v := i
		s.schedule(func() error {
			mu.Lock()
			defer mu.Unlock()
			wrote = append(wrote, v)
			return nil
		})
		time.Sleep(2 * time.Millisecond)
	}
	waitUntil(t, "去抖后写入", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(wrote) > 0
	})
	time.Sleep(60 * time.Millisecond) // 多等一会儿，确认没有第二次
	mu.Lock()
	defer mu.Unlock()
	if len(wrote) != 1 || wrote[0] != 5 {
		t.Errorf("连续 5 次变化只应写最后一次：%v", wrote)
	}
}

func TestSaverFlushWritesNowAndCancelsTimer(t *testing.T) {
	s := fastSaver(nil)
	var n atomic.Int32
	s.schedule(func() error { n.Add(1); return nil })
	s.flush()
	if n.Load() != 1 {
		t.Fatalf("flush 应立即写入：%d", n.Load())
	}
	time.Sleep(60 * time.Millisecond)
	if n.Load() != 1 {
		t.Errorf("flush 后定时器不该再触发：%d", n.Load())
	}
	s.flush() // 没有待写：什么都不做
	if n.Load() != 1 {
		t.Error("没有待写动作时 flush 不该写")
	}
	// 之后还能继续用
	s.schedule(func() error { n.Add(1); return nil })
	waitUntil(t, "再次写入", func() bool { return n.Load() == 2 })
}

func TestSaverReportsErrorsWithoutPanicking(t *testing.T) {
	var got atomic.Value
	s := fastSaver(func(err error) { got.Store(err) })
	s.schedule(func() error { return errors.New("写不进去") })
	waitUntil(t, "错误回调", func() bool { return got.Load() != nil })
	if err := got.Load().(error); err.Error() != "写不进去" {
		t.Errorf("err = %v", err)
	}

	// 没有 onError 时只记诊断日志，不崩
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)
	s2 := fastSaver(nil)
	s2.schedule(func() error { return errors.New("写不进去") })
	s2.flush()
	if !strings.Contains(buf.String(), "写不进去") {
		t.Errorf("没有 onError 时应记诊断日志：%q", buf.String())
	}
}
