package task

import "context"

type Scheduler struct {
	queue chan string
}

func NewScheduler(size int) *Scheduler {
	if size <= 0 {
		size = 1
	}
	return &Scheduler{queue: make(chan string, size)}
}

func (s *Scheduler) Enqueue(ctx context.Context, id string) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case s.queue <- id:
		return nil
	}
}

func (s *Scheduler) Next(ctx context.Context) (string, error) {
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case id := <-s.queue:
		return id, nil
	}
}
