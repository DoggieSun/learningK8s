// Miniature PodWorker Simulation
package main

import (
	"fmt"
	"sync"
	"time"
)

type PodState int

const (
	PodSyncing PodState = iota
	PodTerminating
	PodTerminated
)

type PodWork struct {
	UID   string
	Event string // "update" or "terminate"
}

type PodWorkers struct {
	mu      sync.Mutex
	workers map[string]chan PodWork
	states  map[string]PodState
}

func NewPodWorkers() *PodWorkers {
	return &PodWorkers{
		workers: make(map[string]chan PodWork),
		states:  make(map[string]PodState),
	}
}

func (pw *PodWorkers) UpdatePod(uid string) {
	pw.enqueueWork(uid, "update")
}

func (pw *PodWorkers) TerminatePod(uid string) {
	pw.enqueueWork(uid, "terminate")
}

func (pw *PodWorkers) enqueueWork(uid, event string) {
	pw.mu.Lock()
	defer pw.mu.Unlock()

	if pw.states[uid] == PodTerminated {
		fmt.Printf("Pod %s is already terminated. Ignoring %s.\n", uid, event)
		return
	}

	ch, exists := pw.workers[uid]
	if !exists {
		ch = make(chan PodWork, 10)
		pw.workers[uid] = ch
		pw.states[uid] = PodSyncing
		go pw.worker(uid, ch)
	}

	// 右进左出 切记！
	ch <- PodWork{UID: uid, Event: event}
}

func (pw *PodWorkers) worker(uid string, ch <-chan PodWork) {
	for work := range ch {
		switch work.Event {
		case "update":
			pw.mu.Lock()
			if pw.states[uid] != PodSyncing {
				pw.mu.Unlock()
				fmt.Printf("Pod %s not in syncing state, skipping update.\n", uid)
				continue
			}
			pw.mu.Unlock()

			fmt.Printf("[SYNC] Pod %s is being updated.\n", uid)
			time.Sleep(200 * time.Millisecond)

		case "terminate":
			pw.mu.Lock()
			if pw.states[uid] == PodTerminating || pw.states[uid] == PodTerminated {
				pw.mu.Unlock()
				continue
			}
			pw.states[uid] = PodTerminating
			pw.mu.Unlock()

			fmt.Printf("[TERM] Pod %s is terminating...\n", uid)
			time.Sleep(500 * time.Millisecond)

			pw.mu.Lock()
			pw.states[uid] = PodTerminated
			pw.mu.Unlock()
			fmt.Printf("[DONE] Pod %s is now terminated.\n", uid)
			return
		}
	}
}

func main() {
	pw := NewPodWorkers()

	uids := []string{"podA", "podB", "podC"}

	for _, uid := range uids {
		go func(id string) {
			for i := 0; i < 3; i++ {
				pw.UpdatePod(id)
				time.Sleep(100 * time.Millisecond)
			}
		}(uid)
	}

	time.Sleep(1 * time.Second)

	for _, uid := range uids {
		pw.TerminatePod(uid)
	}

	time.Sleep(1 * time.Second)
	fmt.Println("All pods processed.")
}
