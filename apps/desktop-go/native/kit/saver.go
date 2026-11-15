package kit

import (
	"log"
	"sync"
	"time"
)

// saveDelay 是偏好变化到真正写入之间的去抖间隔：拖动窗格、拖拽列宽每帧都在改值，不能每帧写一次。
const saveDelay = 400 * time.Millisecond

// saver 把「频繁变化的值」去抖后写出去：连续多次 schedule 只执行最后一次。
// 窗口关闭前调用 flush，把还没写出去的改动落下来，所以最后一次拖动不会丢。
type saver struct {
	mu      sync.Mutex
	delay   time.Duration // 0 取 saveDelay；测试里调小
	timer   *time.Timer
	pending func() error
	// onError 在写入失败时调用；nil 时只记诊断日志。偏好写不进去不该打断用户，下次变化会再试。
	onError func(error)
}

// schedule 记下最新的待写动作并重新计时。
func (s *saver) schedule(write func() error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pending = write
	delay := s.delay
	if delay == 0 {
		delay = saveDelay
	}
	if s.timer != nil {
		s.timer.Stop()
	}
	s.timer = time.AfterFunc(delay, s.flush)
}

// flush 立即执行待写动作（没有就什么都不做），并取消计时。
func (s *saver) flush() {
	s.mu.Lock()
	write := s.pending
	s.pending = nil
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	onError := s.onError
	s.mu.Unlock()
	if write == nil {
		return
	}
	if err := write(); err != nil {
		if onError != nil {
			onError(err)
			return
		}
		log.Printf("[native] 保存界面偏好失败：%v", err)
	}
}
