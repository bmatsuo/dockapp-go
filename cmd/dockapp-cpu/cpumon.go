package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// CPU is an abstraction for a CPU core that measures its utilization.  The
// utilization is measured as a fraction from 0.0 to 1.0 meaning completely
// unused and completely saturated.
type CPU interface {
	Name() string
	FracUtil() float64
}

// Constants for CPU mode indices in a Time.InMode value.
const (
	ModeIdle = 3
)

// Delta returns channel that receives deltas in Time values received over c.
// The returned channel is closed afer c is closed.
func Delta(c <-chan []*Time) <-chan []*Time {
	d := make(chan []*Time)
	go func() {
		defer close(d)
		var told []*Time
		var tdelta []*Time
		var _d chan []*Time
		for {
			select {
			case tnew, ok := <-c:
				if !ok {
					return
				}
				tdelta = append([]*Time(nil), tnew...)
				if told != nil {
					for i, t := range told {
						tdelta[i] = tdelta[i].Sub(t)
					}
					_d = d
				}
				told = tnew
			case _d <- tdelta:
				_d = nil
			}
		}
	}()

	return d
}

// Poller periodically measures CPU utilization.
type Poller struct {
	tick  *time.Ticker
	C     chan []*Time
	stop  chan struct{}
	times []*Time
}

// Poll returns a new Poller that has begun polling CPU utilization.
func Poll(dur time.Duration) (*Poller, error) {
	timesInit, err := ReadTime()
	if err != nil {
		return nil, err
	}
	p := &Poller{
		tick:  time.NewTicker(dur),
		C:     make(chan []*Time, 1),
		stop:  make(chan struct{}),
		times: timesInit,
	}
	go p.loop()
	return p, nil
}

// Stop stops polling for CPU utilization.
func (p *Poller) Stop() {
	p.tick.Stop()
	close(p.stop)
}

func (p *Poller) poll() bool {
	times, err := ReadTime()
	if err != nil {
		log.Printf("cpumon: %v", err)
		return false
	}
	p.times = times
	return true
}

func (p *Poller) loop() {
	defer close(p.C)
	var c chan []*Time
	for {
		select {
		case <-p.stop:
			return
		case <-p.tick.C:
			if p.poll() {
				c = p.C
			}
		case c <- p.times:
			c = nil
		}
	}
}

// Time is a measurement of the time spent in each CPU mode.
type Time struct {
	name   string
	InMode []int64
}

// ReadTime opens /proc/stat and reads the times each CPU has spent in each of
// their modes.
func ReadTime() ([]*Time, error) {
	stat, err := os.Open("/proc/stat")
	if err != nil {
		return nil, err
	}
	defer stat.Close()
	return readTime(stat)
}

var matchStatCPU = regexp.MustCompile(`^cpu\d*\s`).Match

func readTime(r io.Reader) ([]*Time, error) {
	var times []*Time
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		if !matchStatCPU(scanner.Bytes()) {
			continue
		}
		pieces := strings.Fields(scanner.Text())
		t := &Time{
			name: pieces[0],
		}
		times = append(times, t)
		for _, piece := range pieces[1:] {
			count, err := strconv.ParseInt(piece, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("unable to parse line: %q", scanner.Bytes())
			}
			t.InMode = append(t.InMode, count)
		}

	}
	if scanner.Err() != nil {
		return nil, scanner.Err()
	}
	return times, nil
}

// Name returns the name of the CPU corresponding to t.
func (t *Time) Name() string {
	return t.name
}

// Sub returns the difference of time measurements in t and t2.
func (t *Time) Sub(t2 *Time) *Time {
	t3 := &Time{
		name:   t.name,
		InMode: append([]int64(nil), t.InMode...),
	}
	for i, dur := range t2.InMode {
		t3.InMode[i] -= dur
	}
	return t3
}

// Frac returns the fraction of time spent in the given mode relative to other
// modes.
func (t *Time) Frac(mode int) float64 {
	idle := float64(t.InMode[mode])
	total := 0.0
	for _, mode := range t.InMode {
		total += float64(mode)
	}
	return idle / total
}

// FracUtil implements the CPU interface.
func (t *Time) FracUtil() float64 {
	return 1 - t.Frac(ModeIdle)
}

// TimeToCPU transforms []*Time values representing the cores of a machine in
// []CPU.
func TimeToCPU(times <-chan []*Time) <-chan []CPU {
	c := make(chan []CPU)
	go func() {
		defer close(c)
		for times := range times {
			var cpus []CPU
			for _, t := range times {
				cpus = append(cpus, t)
			}
			c <- cpus
		}
	}()
	return c
}

// FilterCPU removes the specified cores from slices received over the cpus
// chan.
func FilterCPU(cpus <-chan []CPU, ignore []string) <-chan []CPU {
	if len(ignore) == 0 {
		return cpus
	}

	c := make(chan []CPU)
	go func() {
		defer close(c)
		for cpus := range cpus {
			var _cpus []CPU
			for _, t := range cpus {
				for _, name := range ignore {
					if t.Name() == name {
						continue
					}
					_cpus = append(_cpus, t)
				}
			}
			cpus = _cpus
			c <- cpus
		}
	}()

	return c
}
