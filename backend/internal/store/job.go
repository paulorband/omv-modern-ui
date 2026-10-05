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
	ID        string
	Title     string
	Icon      string
	StartedAt time.Time

	mu         sync.Mutex
	events     []Event
	done       bool
	finishedAt time.Time
	wake       chan struct{}
}

func newJob(id string) *Job {
	return &Job{ID: id, StartedAt: time.Now(), wake: make(chan struct{})}
}

// maxLogEvents bounds the stored output of one install; done and error are always kept (R10.5).
const maxLogEvents = 5000

func (j *Job) emit(e Event) {
	j.mu.Lock()
	defer j.mu.Unlock()
	final := e.Type == "done" || e.Type == "error"
	if len(j.events) >= maxLogEvents && !final {
		return
	}
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

// JobSummary is the list item of GET /store/installs (SPEC-10).
type JobSummary struct {
	ID         string     `json:"id"`
	Title      string     `json:"title"`
	Icon       string     `json:"icon,omitempty"`
	State      string     `json:"state"` // running | done | error
	StartedAt  time.Time  `json:"startedAt"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
	Message    string     `json:"message,omitempty"`
}

// Summary snapshots the job for listing.
func (j *Job) Summary() JobSummary {
	j.mu.Lock()
	defer j.mu.Unlock()
	s := JobSummary{ID: j.ID, Title: j.Title, Icon: j.Icon, State: "running", StartedAt: j.StartedAt}
	if s.Title == "" {
		s.Title = j.ID
	}
	for i := len(j.events) - 1; i >= 0; i-- {
		if m := j.events[i].Message; m != "" {
			s.Message = m
			break
		}
	}
	if j.done {
		t := j.finishedAt
		s.FinishedAt = &t
		s.State = "done"
		if last := j.events[len(j.events)-1]; last.Type == "error" {
			s.State = "error"
		}
	}
	return s
}
