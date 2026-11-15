package aiflow

// 查询向量缓存与并发合并（对应 services/knowledge-ai/query-vectors.ts）。
//
//   - 缓存键是 (配置, 查询) 的单向指纹，凭证与查询明文不进缓存键、日志或磁盘；
//   - 命中缓存：LRU 刷新并续用，5 分钟过期，最多 100 条；
//   - 相同键的并发请求合并成一次 embeddings 调用；调用方各自可取消，全部取消才中止底层请求；
//   - 底层请求被取消（全部调用方离开或 Reset）时结果不入缓存。

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sync"
	"time"
)

const (
	queryVectorMaxCache = 100
	queryVectorTTL      = 5 * time.Minute
)

type queryVectorEntry struct {
	cancel context.CancelFunc // 中止底层 embeddings 请求
	users  int
	done   chan struct{}
	value  []float64
	err    error
}

type cachedQueryVector struct {
	value   []float64
	expires time.Time
}

// QueryVectors 是进程内的查询向量缓存；零值可用。
type QueryVectors struct {
	// Now 取当前时间，测试可注入；nil 用 time.Now。
	Now func() time.Time

	mu      sync.Mutex
	pending map[string]*queryVectorEntry
	cache   map[string]cachedQueryVector
	order   []string // 缓存键，最久未用在前（JS Map 的插入序）
}

func (q *QueryVectors) now() time.Time {
	if q.Now != nil {
		return q.Now()
	}
	return time.Now()
}

func (q *QueryVectors) init() {
	if q.pending == nil {
		q.pending = map[string]*queryVectorEntry{}
		q.cache = map[string]cachedQueryVector{}
	}
}

// queryVectorKey 把配置与查询折成单向指纹。
func queryVectorKey(cfg AIConfig, query string) string {
	raw, _ := json.Marshal([]string{cfg.ID, cfg.Provider, cfg.APIProtocol, cfg.APIURL, cfg.Model, cfg.APIKey, query})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// Reset 对应 resetQueryVectors：中止所有在途请求并清空缓存（设置里的 embedding 模型变更后调用）。
func (q *QueryVectors) Reset() {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, entry := range q.pending {
		entry.cancel()
	}
	q.pending = map[string]*queryVectorEntry{}
	q.cache = map[string]cachedQueryVector{}
	q.order = nil
}

// touch 把键移到队尾（最近使用）；调用方持锁。
func (q *QueryVectors) touch(key string) {
	q.removeOrder(key)
	q.order = append(q.order, key)
}

func (q *QueryVectors) removeOrder(key string) {
	for i, existing := range q.order {
		if existing == key {
			q.order = append(q.order[:i], q.order[i+1:]...)
			return
		}
	}
}

// store 写缓存并按容量淘汰最久未用的；调用方持锁。
func (q *QueryVectors) store(key string, value []float64) {
	if _, exists := q.cache[key]; !exists {
		q.order = append(q.order, key)
	}
	q.cache[key] = cachedQueryVector{value: value, expires: q.now().Add(queryVectorTTL)}
	for len(q.cache) > queryVectorMaxCache {
		oldest := q.order[0]
		q.order = q.order[1:]
		delete(q.cache, oldest)
	}
}

// Get 对应 queryVector：返回查询文本的 L2 归一化向量（调用方拿到的是副本）。
func (q *QueryVectors) Get(ctx context.Context, r *Runner, cfg AIConfig, query string) ([]float64, error) {
	if ctx.Err() != nil {
		return nil, ErrAborted
	}
	key := queryVectorKey(cfg, query)
	if ctx.Err() != nil {
		return nil, ErrAborted
	}

	q.mu.Lock()
	q.init()
	if hit, ok := q.cache[key]; ok {
		if hit.expires.After(q.now()) {
			q.touch(key)
			value := append([]float64(nil), hit.value...)
			q.mu.Unlock()
			return value, nil
		}
		delete(q.cache, key)
		q.removeOrder(key)
	}
	entry := q.pending[key]
	if entry == nil {
		embedCtx, cancel := context.WithCancel(context.Background())
		entry = &queryVectorEntry{cancel: cancel, done: make(chan struct{})}
		q.pending[key] = entry
		go q.run(embedCtx, r, cfg, query, key, entry)
	}
	entry.users++
	q.mu.Unlock()

	select {
	case <-entry.done:
		q.mu.Lock()
		entry.users--
		q.mu.Unlock()
		// 结果与取消同时就绪时 select 会随机选一支；统一按「已取消」处理，与 cancellable 的事后检查一致
		if ctx.Err() != nil {
			return nil, ErrAborted
		}
		if entry.err != nil {
			return nil, entry.err
		}
		return append([]float64(nil), entry.value...), nil
	case <-ctx.Done():
		q.mu.Lock()
		entry.users--
		if entry.users == 0 && q.pending[key] == entry {
			delete(q.pending, key)
			entry.cancel()
		}
		q.mu.Unlock()
		return nil, ErrAborted
	}
}

// run 执行底层的 embeddings 请求，结束后写缓存并唤醒所有等待者。
func (q *QueryVectors) run(embedCtx context.Context, r *Runner, cfg AIConfig, query, key string, entry *queryVectorEntry) {
	vectors, err := r.EmbedTexts(embedCtx, cfg, []string{query})
	var value []float64
	if err == nil {
		if len(vectors) == 0 {
			err = errors.New("查询向量为空")
		} else {
			value = vectors[0]
		}
	}

	q.mu.Lock()
	defer q.mu.Unlock()
	if err == nil && embedCtx.Err() == nil {
		q.store(key, value)
	}
	if q.pending[key] == entry {
		delete(q.pending, key)
	}
	entry.value, entry.err = value, err
	entry.cancel() // 释放 embedCtx 的资源；此时请求已结束，不影响结果
	close(entry.done)
}
