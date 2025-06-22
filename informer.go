package main

import (
	"crypto/sha256"
	"fmt"
	"time"
)

// 模拟的对象资源
var podStore = []string{"pod-a", "pod-b", "pod-c"}

// Reflector 模拟结构
type Reflector struct {
	resourceVersion int
	resyncPeriod    time.Duration
	lastSyncTime    time.Time
	indexer         *Indexer // 新增本地缓存 indexer
}

// List 拉取当前所有资源状态
func (r *Reflector) List() ([]string, int) {
	fmt.Println("[List] 拉取全量 Pod")
	r.resourceVersion = int(time.Now().Unix())
	r.lastSyncTime = time.Now()
	if r.indexer != nil {
		r.indexer.Replace(podStore)
	}
	return podStore, r.resourceVersion
}

// WatchEvent 模拟 API Server 返回的事件
type WatchEvent struct {
	Type   string // ADDED, MODIFIED, DELETED
	Object string
}

// Watch 启动监听资源变化的协程
func (r *Reflector) Watch(eventChan chan Delta) {
	fmt.Println("[Watch] 开始监听 Pod 变化 (模拟 chunked HTTP 数据流)")
	watchStream := make(chan WatchEvent)

	// 模拟 API Server chunked HTTP 输出，每个事件是一个 chunk
	go func() {
		time.Sleep(2 * time.Second)
		fmt.Println("[Chunk] -> {\"type\": \"ADDED\", \"object\": \"pod-d\"}")
		watchStream <- WatchEvent{Type: "ADDED", Object: "pod-d"}

		time.Sleep(1 * time.Second)
		fmt.Println("[Chunk] -> {\"type\": \"MODIFIED\", \"object\": \"pod-d\"}")
		watchStream <- WatchEvent{Type: "MODIFIED", Object: "pod-d"}

		time.Sleep(1 * time.Second)
		fmt.Println("[Chunk] -> {\"type\": \"DELETED\", \"object\": \"pod-a\"}")
		watchStream <- WatchEvent{Type: "DELETED", Object: "pod-a"}
	}()

	// Reflector 接收并转换为 Delta
	go func() {
		for we := range watchStream {
			delta := ConvertWatchEventToDelta(we)
			if r.indexer != nil && r.indexer.IsConsistent(delta.Object, delta.Type) {
				fmt.Printf("[Watch] 忽略一致事件 [%s] %s（内容未变）\n", delta.Type, delta.Object)
				continue
			}
			eventChan <- delta
			if r.indexer != nil {
				switch delta.Type {
				case Add, Update:
					r.indexer.Add(delta.Object)
				case Delete:
					r.indexer.Delete(delta.Object)
				}
			}
		}
	}()
}

// ConvertWatchEventToDelta 映射 watch.Event -> Delta
func ConvertWatchEventToDelta(we WatchEvent) Delta {
	switch we.Type {
	case "ADDED":
		return Delta{Type: Add, Object: we.Object}
	case "MODIFIED":
		return Delta{Type: Update, Object: we.Object}
	case "DELETED":
		return Delta{Type: Delete, Object: we.Object}
	default:
		return Delta{Type: Sync, Object: we.Object} // fallback
	}
}

func (r *Reflector) ShouldResync() bool {
	return time.Since(r.lastSyncTime) >= r.resyncPeriod
}

// Delta 类型枚举
type DeltaType string

const (
	Add    DeltaType = "Add"
	Update DeltaType = "Update"
	Delete DeltaType = "Delete"
	Sync   DeltaType = "Sync"
)

// Delta 表示一个变更事件
type Delta struct {
	Type   DeltaType
	Object string
}

// DeltaFIFO 支持结构化 Delta 入队的简化版
type DeltaFIFO struct {
	queue   []Delta
	seenSet map[string]bool
}

func NewDeltaFIFO() *DeltaFIFO {
	return &DeltaFIFO{
		queue:   make([]Delta, 0),
		seenSet: make(map[string]bool),
	}
}

func (d *DeltaFIFO) Add(delta Delta) {
	key := delta.Object
	if d.seenSet[key] {
		fmt.Printf("[DeltaFIFO] 已存在 key=%s，跳过重复事件\n", key)
		return
	}
	fmt.Printf("[DeltaFIFO] 入队: [%s] %s\n", delta.Type, delta.Object)
	d.queue = append(d.queue, delta)
	d.seenSet[key] = true
}

func (d *DeltaFIFO) Pop() *Delta {
	if len(d.queue) == 0 {
		return nil
	}
	delta := d.queue[0]
	d.queue = d.queue[1:]
	delete(d.seenSet, delta.Object)
	return &delta
}

// Indexer 是一个简单的本地缓存（支持 hash 校验）
type Indexer struct {
	store map[string]string // key -> hash(obj)
}

func NewIndexer() *Indexer {
	return &Indexer{
		store: make(map[string]string),
	}
}

func (i *Indexer) hash(obj string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(obj)))
}

func (i *Indexer) Add(obj string) {
	h := i.hash(obj)
	fmt.Printf("[Indexer] 缓存添加/更新: %s (hash=%s)\n", obj, h)
	i.store[obj] = h
}

func (i *Indexer) Delete(obj string) {
	fmt.Printf("[Indexer] 缓存删除: %s\n", obj)
	delete(i.store, obj)
}

func (i *Indexer) Replace(objs []string) {
	fmt.Println("[Indexer] 替换本地缓存为最新 List 状态")
	i.store = make(map[string]string)
	for _, obj := range objs {
		i.store[obj] = i.hash(obj)
	}
}

func (i *Indexer) List() []string {
	var out []string
	for k := range i.store {
		out = append(out, k)
	}
	return out
}

func (i *Indexer) IsConsistent(obj string, typ DeltaType) bool {
	h := i.hash(obj)
	prev, exists := i.store[obj]
	if typ == Delete {
		return !exists
	}
	return exists && prev == h
}

func main() {
	indexer := NewIndexer()
	r := &Reflector{
		resyncPeriod: 4 * time.Second,
		indexer:      indexer,
	}
	eventChan := make(chan Delta)
	deltaFIFO := NewDeltaFIFO()

	// Step 1: List
	pods, rv := r.List()
	fmt.Printf("当前 Pods: %v (rv=%d)\n", pods, rv)

	// Step 2: Watch
	r.Watch(eventChan)

	// Step 3: 收到事件后加入 DeltaFIFO
	go func() {
		for i := 0; i < 3; i++ {
			event := <-eventChan
			deltaFIFO.Add(event)
		}
	}()

	// Step 4: 模拟事件处理器 + Resync 检查
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		for range ticker.C {
			if r.ShouldResync() {
				fmt.Println("[Resync] 到达 Resync 周期，重新 List")
				pods, _ := r.List()
				for _, pod := range pods {
					deltaFIFO.Add(Delta{Type: Sync, Object: pod})
				}
			}
		}
	}()

	time.Sleep(7 * time.Second)
	for i := 0; i < 6; i++ {
		d := deltaFIFO.Pop()
		if d != nil {
			fmt.Printf("[EventHandler] 处理事件: [%s] %s\n", d.Type, d.Object)
		}
	}

	fmt.Printf("[Indexer] 当前缓存状态: %v\n", indexer.List())
	fmt.Println("Reflector + FIFO + Resync + WatchEvent + ChunkedHTTP + Indexer缓存 + 一致性校验示例结束")
}
