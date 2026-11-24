package servicestest

import (
	"context"
	"errors"
	"sync"

	"guizhi-mygo-shell/native/services"
)

// BackgroundImagesFake 是 services.BackgroundImages 的假实现：内存里存图片表，选图结果可注入，读取与选图次数可查。零值可用。
//
// 选图默认返回 ("", nil)（相当于用户取消）；PickFunc 非空时由它决定结果，可用来模拟选中并存出文件名、保存失败等。
// 并发安全：界面线程与后台线程可同时调用。
type BackgroundImagesFake struct {
	// PickFunc 非空时接管 PickAndSave 的结果（在持锁之外调用，可以自己阻塞或访问 Put）。
	PickFunc func(ctx context.Context) (string, error)

	mu       sync.Mutex
	images   map[string][]byte
	pickErr  error
	readErr  error
	picks    int
	reads    int
	readName []string
}

var _ services.BackgroundImages = (*BackgroundImagesFake)(nil)

// ErrBackgroundImageNotFound 是读取一个没有 Put 过的文件名时的错误（假错误全英文）。
var ErrBackgroundImageNotFound = errors.New("background image not found")

// Put 往内存图片表里放一张图（拷贝字节）。
func (f *BackgroundImagesFake) Put(name string, data []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.images == nil {
		f.images = map[string][]byte{}
	}
	f.images[name] = append([]byte(nil), data...)
}

// FailPick 让 PickAndSave 返回 err（优先于 PickFunc）；传 nil 取消。
func (f *BackgroundImagesFake) FailPick(err error) { f.mu.Lock(); f.pickErr = err; f.mu.Unlock() }

// FailRead 让 Read 返回 err（优先于图片表）；传 nil 取消。
func (f *BackgroundImagesFake) FailRead(err error) { f.mu.Lock(); f.readErr = err; f.mu.Unlock() }

// Picks 是 PickAndSave 的调用次数；Reads 是 Read 的调用次数；ReadNames 是每次 Read 的文件名（按顺序）。
func (f *BackgroundImagesFake) Picks() int { f.mu.Lock(); defer f.mu.Unlock(); return f.picks }
func (f *BackgroundImagesFake) Reads() int { f.mu.Lock(); defer f.mu.Unlock(); return f.reads }
func (f *BackgroundImagesFake) ReadNames() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.readName...)
}

func (f *BackgroundImagesFake) PickAndSave(ctx context.Context) (string, error) {
	f.mu.Lock()
	f.picks++
	err, pick := f.pickErr, f.PickFunc
	f.mu.Unlock()
	if err != nil {
		return "", err
	}
	if pick != nil {
		return pick(ctx)
	}
	return "", nil
}

func (f *BackgroundImagesFake) Read(ctx context.Context, fileName string) ([]byte, error) {
	f.mu.Lock()
	f.reads++
	f.readName = append(f.readName, fileName)
	err := f.readErr
	data, ok := f.images[fileName]
	f.mu.Unlock()
	if err != nil {
		return nil, err
	}
	if cerr := ctx.Err(); cerr != nil {
		return nil, cerr
	}
	if !ok {
		return nil, ErrBackgroundImageNotFound
	}
	return append([]byte(nil), data...), nil
}
