package battery

import (
	"log"
	"sync"
	"time"
)

// Guage is an interface that can derive metrics for the computer's
// battery.
type Guage interface {
	BatteryMetrics() (*Metrics, error)
}

// StateNotifier complements a Guage by sending over notifications when
// the battery "connected" state has changed.
type StateNotifier interface {
	BatteryStateChange(notifications chan<- struct{}) (stop func())
}

// Profiler is a Guage that periodically polls an underlying
// Guage.
type Profiler struct {
	g      Guage
	change chan struct{}
	stop   chan struct{}

	mut     sync.RWMutex
	metrics *Metrics
}

// NewProfiler returns a new Profiler that periodically polls g.
func NewProfiler(g Guage) *Profiler {
	b := new(Profiler)
	b.stop = make(chan struct{})
	b.g = g
	return b
}

// Start begins polling the underlying Guage at the specified interval
// and sends Metrics over c.
func (b *Profiler) Start(interval time.Duration, c chan<- *Metrics) {
	watchStop := b.watchState()
	defer watchStop()

	tick := time.NewTicker(interval)
	defer tick.Stop()

	refreshing := false
	refreshed := make(chan error, 1)
	refresh := func() { refreshed <- b.refreshMetrics() }

	refreshing = true
	refresh()

	for {
		// either stop or refresh the metrics and attempt to notify c
		select {
		case <-b.stop:
			return
		case <-b.change:
			if !refreshing {
				refreshing = true
				go refresh()
			}
		case <-tick.C:
			if !refreshing {
				refreshing = true
				go refresh()
			}
		case err := <-refreshed:
			refreshing = false
			if err != nil {
				log.Print(err)
			}
			select {
			case c <- b.batteryMetrics():
			default:
			}
		}
	}
}

func (b *Profiler) watchState() func() {
	if notf, ok := b.g.(StateNotifier); ok {
		b.change = make(chan struct{})
		return notf.BatteryStateChange(b.change)
	}
	return func() {} // noop
}

// Stop prevents future poll events.
func (b *Profiler) Stop() {
	close(b.stop)
}

func (b *Profiler) refreshMetrics() error {
	m, err := b.g.BatteryMetrics()
	if err != nil {
		return err
	}
	b.mut.Lock()
	b.metrics = m
	b.mut.Unlock()
	return nil
}

func (b *Profiler) batteryMetrics() *Metrics {
	b.mut.RLock()
	m := b.metrics
	b.mut.RUnlock()
	return m
}

// BatteryMetrics implements the Guage interface and returns cached
// metrics from the underlying Guage.
func (b *Profiler) BatteryMetrics() (*Metrics, error) {
	return b.batteryMetrics(), nil
}
