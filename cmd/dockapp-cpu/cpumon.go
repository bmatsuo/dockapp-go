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

/*
func main() {
	p, err := Poll(time.Second)
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		time.Sleep(10 * time.Second)
		p.Stop()
	}()
	for delta := range Delta(p.C) {
		fmt.Println()
		for _, cpu := range delta {
			fmt.Printf("%s %.03g\n", cpu.Name, UtilFrac(cpu))
		}
	}
}
*/

const (
	ModeIdle = 3
)

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

type Poller struct {
	tick  *time.Ticker
	C     chan []*Time
	times []*Time
}

func Poll(dur time.Duration) (*Poller, error) {
	timesInit, err := ReadTime()
	if err != nil {
		return nil, err
	}
	p := &Poller{
		tick:  time.NewTicker(dur),
		C:     make(chan []*Time, 1),
		times: timesInit,
	}
	go p.loop()
	return p, nil
}

func (p *Poller) Stop() {
	p.tick.Stop()
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
	var c chan []*Time
	for {
		select {
		case <-p.tick.C:
			if p.poll() {
				c = p.C
			}
		case c <- p.times:
			c = nil
		}
	}
}

type Time struct {
	Name   string
	InMode []int64
}

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
			Name: pieces[0],
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

func (t1 *Time) Sub(t2 *Time) *Time {
	t3 := &Time{
		Name:   t1.Name,
		InMode: append([]int64(nil), t1.InMode...),
	}
	for i, dur := range t2.InMode {
		t3.InMode[i] -= dur
	}
	return t3
}

func (t *Time) Frac(mode int) float64 {
	idle := float64(t.InMode[mode])
	total := 0.0
	for _, mode := range t.InMode {
		total += float64(mode)
	}
	return idle / total
}

func UtilFrac(t *Time) float64 {
	return 1 - t.Frac(ModeIdle)
}
