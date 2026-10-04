package store

import (
	"context"
	"strings"
	"sync"
	"time"
)

// Event is one progress item of an install (SSE: pull, starting, log, done, error).
type Event struct {
	Type    string `json:"type"`
	Message string `json:"message,omitempty"`
}

// Job records the events of one install so late SSE subscribers can replay them.
type Job struct {
	ID string

	mu         sync.Mutex
	events     []Event
	done       bool
	finishedAt time.Time
	wake       chan struct{}
}

func newJob(id string) *Job { return &Job{ID: id, wake: make(chan struct{})} }

func (j *Job) emit(e Event) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.events = append(j.events, e)
	if e.Type == "done" || e.Type == "error" {
		j.done, j.finishedAt = true, time.Now()
	}
	close(j.wake)
	j.wake = make(chan struct{})
}

// Done reports whether the job reached done or error.
func (j *Job) Done() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.done
}

// Stream replays past events then follows new ones until the job ends or ctx is cancelled.
func (j *Job) Stream(ctx context.Context, fn func(Event) error) error {
	idx := 0
	for {
		j.mu.Lock()
		pending := append([]Event(nil), j.events[idx:]...)
		idx = len(j.events)
		done, wake := j.done, j.wake
		j.mu.Unlock()

		for _, e := range pending {
			if err := fn(e); err != nil {
				return err
			}
		}
		if done {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-wake:
		}
	}
}

// lineEvent classifies one line of compose output.
func lineEvent(line string) Event {
	if strings.Contains(strings.ToLower(line), "pull") {
		return Event{Type: "pull", Message: line}
	}
	return Event{Type: "log", Message: line}
}
