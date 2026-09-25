package syncengine

import "sync"

// announcer hands state changes to Options.OnJob in the order they happened.
//
// Every change is made under the engine lock and announced after it is
// released, so that a slow observer never stalls the scheduler. Released locks
// do not keep their order, though: the goroutine that re-queued a job (Enqueue,
// RetryDeadLetter) could be overtaken by the worker that picked the job up at
// once, and an observer heard `running`, or even `done`, before `queued` — and
// kept showing the job as queued (GIT-US-0146).
//
// So each change takes a turn while it still holds the engine lock, and the
// announcements run strictly in turn order. A turn is taken under e.mu and
// waited for under the announcer's own lock, never both at once, and no lock is
// held while OnJob runs. Every turn that is taken must be run, or every later
// announcement waits for it forever.
type announcer struct {
	onJob func(Job)

	// issued is the next turn to hand out. Guarded by the engine lock.
	issued uint64

	mu   sync.Mutex
	cond *sync.Cond
	// next is the turn whose announcement may run now. Guarded by mu.
	next uint64
}

// init binds the callback. A nil callback turns the announcer into a no-op.
func (a *announcer) init(onJob func(Job)) {
	a.onJob = onJob
	a.cond = sync.NewCond(&a.mu)
}

// reserveLocked takes the next turn. The caller holds the engine lock, in the
// same critical section as the change it will announce.
func (a *announcer) reserveLocked() uint64 {
	if a.onJob == nil {
		return 0
	}
	turn := a.issued
	a.issued++
	return turn
}

// run waits for its turn, hands jobs to OnJob and passes the turn on. The
// caller must not hold the engine lock.
func (a *announcer) run(turn uint64, jobs ...Job) {
	if a.onJob == nil {
		return
	}
	a.mu.Lock()
	for a.next != turn {
		a.cond.Wait()
	}
	a.mu.Unlock()

	defer func() {
		a.mu.Lock()
		a.next++
		a.cond.Broadcast()
		a.mu.Unlock()
	}()
	for _, job := range jobs {
		a.onJob(job)
	}
}
