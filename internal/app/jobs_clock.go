package app

import "time"

type durableJobTimer interface {
	channelValue() <-chan time.Time
	stop() bool
}

type durableJobClock interface {
	now() time.Time
	newTimer(wait time.Duration) durableJobTimer
}

type realDurableJobClock struct{}

func (realDurableJobClock) now() time.Time {
	return time.Now().UTC()
}

func (realDurableJobClock) newTimer(wait time.Duration) durableJobTimer {
	return &realDurableJobTimer{timer: time.NewTimer(wait)}
}

type realDurableJobTimer struct {
	timer *time.Timer
}

func (timer *realDurableJobTimer) channelValue() <-chan time.Time {
	return timer.timer.C
}

func (timer *realDurableJobTimer) stop() bool {
	return timer.timer.Stop()
}
