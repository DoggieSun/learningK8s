// Step 1: Basic PodWorker Simulation (already implemented)
// Step 2: Introduce PodPhase and state transitions
// Step 3: Add backoff & retry for updates
// Step 4: Implement a simple rate-limiting queue
// Step 5: Simulate container lifecycle (Creating, Running, etc.)
// Step 6: Add readiness/liveness probes (simulated)
// Step 7: Add metrics & logging abstraction
// Step 8: GC mechanism for terminated pods
// Step 9: Add graceful termination delay to simulate SIGTERM/SIGKILL
// Step 10: Simulate signal handler inside container (optional exit delay)
// ---

package main

import (
	"fmt"
	"math/rand"
	"sync"
	"time"
)

type PodPhase string

const (
	Pending   PodPhase = "Pending"
	Creating  PodPhase = "Creating"
	Running   PodPhase = "Running"
	Succeeded PodPhase = "Succeeded"
	Failed    PodPhase = "Failed"
	Unknown   PodPhase = "Unknown"
)

type PodState int

const (
	PodSyncing PodState = iota
	PodTerminating
	PodTerminated
)

type PodStatus struct {
	Phase PodPhase
	Ready bool
	Live  bool
}

type PodWork struct {
	UID   string
	Event string // "update" or "terminate"
}

// Simple metrics counter
var (
	metricMu        sync.Mutex
	totalUpdates    int
	totalFailures   int
	totalTerminated int
)

func recordMetric(name string) {
	metricMu.Lock()
	defer metricMu.Unlock()
	switch name {
	case "update":
		totalUpdates++
	case "fail":
		totalFailures++
	case "terminate":
		totalTerminated++
	}
}

func printMetrics() {
	metricMu.Lock()
	defer metricMu.Unlock()
	fmt.Printf("\n--- Metrics Summary ---\n")
	fmt.Printf("Total Updates: %d\n", totalUpdates)
	fmt.Printf("Total Failures: %d\n", totalFailures)
	fmt.Printf("Total Terminated: %d\n", totalTerminated)
	fmt.Printf("------------------------\n")
}

// RateLimiter 用于限制每个 pod 的最大处理频率（令牌桶模型的简化版）
type RateLimiter struct {
	mu         sync.Mutex
	lastAccess map[string]time.Time
	minGap     time.Duration
}

func NewRateLimiter(minGap time.Duration) *RateLimiter {
	return &RateLimiter{
		lastAccess: make(map[string]time.Time),
		minGap:     minGap,
	}
}

func (r *RateLimiter) Allow(uid string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	if last, ok := r.lastAccess[uid]; ok {
		if now.Sub(last) < r.minGap {
			return false
		}
	}
	r.lastAccess[uid] = now
	return true
}

type PodWorkers struct {
	mu          sync.Mutex
	workers     map[string]chan PodWork
	states      map[string]PodState
	phases      map[string]PodPhase
	status      map[string]*PodStatus
	rateLimiter *RateLimiter
}

func NewPodWorkers() *PodWorkers {
	pw := &PodWorkers{
		workers:     make(map[string]chan PodWork),
		states:      make(map[string]PodState),
		phases:      make(map[string]PodPhase),
		status:      make(map[string]*PodStatus),
		rateLimiter: NewRateLimiter(300 * time.Millisecond),
	}
	// 启动 GC 后台任务
	go pw.gcLoop(2 * time.Second)
	return pw
}

func (pw *PodWorkers) gcLoop(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		pw.mu.Lock()
		for uid, state := range pw.states {
			if state == PodTerminated {
				fmt.Printf("[GC] Cleaning up pod %s\n", uid)
				delete(pw.states, uid)
				delete(pw.phases, uid)
				delete(pw.status, uid)
				delete(pw.workers, uid)
			}
		}
		pw.mu.Unlock()
	}
}

func (pw *PodWorkers) UpdatePod(uid string) {
	recordMetric("update")
	pw.enqueueWork(uid, "update")
}

func (pw *PodWorkers) TerminatePod(uid string) {
	recordMetric("terminate")
	pw.enqueueWork(uid, "terminate")
}

func (pw *PodWorkers) enqueueWork(uid, event string) {
	pw.mu.Lock()
	defer pw.mu.Unlock()

	if pw.states[uid] == PodTerminated {
		fmt.Printf("Pod %s is already terminated. Ignoring %s.\n", uid, event)
		return
	}

	if !pw.rateLimiter.Allow(uid) && event == "update" {
		fmt.Printf("[DROP] Rate limit: dropping update for pod %s\n", uid)
		return
	}

	ch, exists := pw.workers[uid]
	if !exists {
		ch = make(chan PodWork, 10)
		pw.workers[uid] = ch
		pw.states[uid] = PodSyncing
		pw.phases[uid] = Pending
		pw.status[uid] = &PodStatus{Phase: Pending, Ready: false, Live: true}
		go pw.worker(uid, ch)
	}

	ch <- PodWork{UID: uid, Event: event}
}

// Step 11: Skip sync if PodSpec is unchanged

func (pw *PodWorkers) worker(uid string, ch <-chan PodWork) {
	const maxRetries = 3
	for work := range ch {
		switch work.Event {
		case "update":
			pw.mu.Lock()
			if pw.states[uid] != PodSyncing {
				pw.mu.Unlock()
				fmt.Printf("Pod %s not in syncing state, skipping update.\n", uid)
				continue
			}
			enteringCreate := false
			if pw.phases[uid] == Pending {
				pw.phases[uid] = Creating
				enteringCreate = true
			}
			pw.mu.Unlock()

			if enteringCreate {
				fmt.Printf("[PHASE] Pod %s is creating...\n", uid)
				time.Sleep(150 * time.Millisecond)
			}

			retries := 0
			for retries <= maxRetries {
				// 模拟检查 PodSpec 是否真的变化
				pw.mu.Lock()
				specChanged := simulateSpecChanged()
				pw.mu.Unlock()
				if !specChanged {
					fmt.Printf("[SKIP] Pod %s spec unchanged. Skipping update.", uid)
					break
				}

				ok := simulateUpdate(uid)
				if ok {
					pw.mu.Lock()
					pw.phases[uid] = Running
					pw.status[uid].Phase = Running
					pw.status[uid].Ready = simulateReadiness()
					pw.status[uid].Live = simulateLiveness()
					pw.mu.Unlock()
					fmt.Printf("[SYNC] Pod %s is now running. Ready=%v Live=%v\n",
						uid, pw.status[uid].Ready, pw.status[uid].Live)
					break
				} else {
					fmt.Printf("[RETRY] Pod %s failed to start. Attempt %d\n", uid, retries+1)
					time.Sleep(time.Duration(100*(1<<retries)) * time.Millisecond)
					retries++
				}
			}
			if retries > maxRetries {
				recordMetric("fail")
				pw.mu.Lock()
				pw.phases[uid] = Failed
				pw.status[uid].Phase = Failed
				pw.status[uid].Live = false
				pw.mu.Unlock()
				fmt.Printf("[FAIL] Pod %s failed after %d retries. Phase: %s\n", uid, maxRetries, pw.phases[uid])
			}

		case "terminate":
			pw.mu.Lock()
			if pw.states[uid] == PodTerminating || pw.states[uid] == PodTerminated {
				pw.mu.Unlock()
				continue
			}
			pw.states[uid] = PodTerminating
			pw.mu.Unlock()

			handled := simulateSigtermHandling(uid)
			if handled {
				time.Sleep(50 * time.Millisecond)
				fmt.Printf("[TERM] Pod %s now sending SIGKILL...", uid)
				time.Sleep(200 * time.Millisecond)
			}

			pw.mu.Lock()
			pw.states[uid] = PodTerminated
			pw.phases[uid] = Succeeded
			pw.status[uid].Phase = Succeeded
			pw.status[uid].Ready = false
			pw.status[uid].Live = false
			pw.mu.Unlock()
			fmt.Printf("[DONE] Pod %s terminated. Phase: %s\n", uid, pw.phases[uid])
			return
		}
	}
}

func simulateUpdate(uid string) bool {
	return rand.Float32() < 0.7
}

func simulateReadiness() bool {
	return rand.Float32() < 0.9
}

func simulateLiveness() bool {
	return rand.Float32() < 0.95
}

// 模拟 PodSpec 是否发生变化
func simulateSpecChanged() bool {
	return rand.Float32() < 0.6 // 60% 概率有变化
}

// 模拟容器是否处理 SIGTERM
func simulateContainerHandlesSigterm() bool {
	return rand.Float32() < 0.5
}

func simulateSigtermHandling(uid string) bool {
	fmt.Printf("[TERM] Pod %s received SIGTERM...", uid)
	if simulateContainerHandlesSigterm() {
		fmt.Printf("[APP] Pod %s handling SIGTERM inside container (cleanup in progress)...", uid)
		time.Sleep(250 * time.Millisecond)
		return true
	} else {
		fmt.Printf("[APP] Pod %s did not handle SIGTERM, exiting immediately.", uid)
		return false
	}
}

// func main() {
// 	rand.Seed(time.Now().UnixNano())
// 	pw := NewPodWorkers()
// 	uids := []string{"podA", "podB", "podC"}

// 	for _, uid := range uids {
// 		go func(id string) {
// 			for i := 0; i < 10; i++ {
// 				pw.UpdatePod(id)
// 				time.Sleep(80 * time.Millisecond)
// 			}
// 		}(uid)
// 	}

// 	time.Sleep(1 * time.Second)

// 	for _, uid := range uids {
// 		pw.TerminatePod(uid)
// 	}

// 	time.Sleep(3 * time.Second)
// 	printMetrics()
// 	fmt.Println("All pods processed.")
// }
