package main

import (
	"bytes"
	"fmt"
	"log"
	"math"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/AmandaCameron/gobar/utils/dbus/upower"
	"launchpad.net/~jamesh/go-dbus/trunk"
)

type BatteryProfiler struct {
	conn    *dbus.Connection
	upower  *upower.UPower
	dev     *upower.Device
	connect chan struct{}
	stop    chan struct{}
	ping    chan chan *BatteryMetrics

	mut     sync.Mutex
	metrics *BatteryMetrics
}

func NewBatteryProfiler() (*BatteryProfiler, error) {
	var err error
	b := new(BatteryProfiler)
	b.connect = make(chan struct{})
	b.stop = make(chan struct{})
	b.ping = make(chan chan *BatteryMetrics)
	b.conn, err = dbus.Connect(dbus.SystemBus)
	if err != nil {
		return nil, fmt.Errorf("connect: %v", err)
	}
	b.upower = upower.New(b.conn)
	b.dev, err = b.upower.GetBattery()
	if err != nil {
		return nil, fmt.Errorf("battery: %v", err)
	}
	return b, nil
}

func (b *BatteryProfiler) Start(interval time.Duration, c chan<- *BatteryMetrics) {
	b.watchConnect()
	tick := time.NewTicker(interval)
	defer tick.Stop()
	var ping chan<- *BatteryMetrics
	for {
		// either stop or refresh the metrics and attempt to notify c
		select {
		case <-b.stop:
			return
		case ping = <-b.ping:
		case <-b.connect:
		case <-tick.C:
		}
		err := b.refreshMetrics()
		if err != nil {
			log.Print(err)
		}
		m := b.metrics
		select {
		case c <- m:
		default:
		}
		if ping != nil {
			ping <- m
			ping = nil
		}
	}
}

func (b *BatteryProfiler) Stop() {
	close(b.stop)
}

func (b *BatteryProfiler) watchConnect() {
	b.dev.Connect(func(*upower.Device) {
		b.connect <- struct{}{}
	})
}

func (b *BatteryProfiler) refreshMetrics() error {
	var m BatteryMetrics
	var err error
	m.State, err = b.dev.State()
	if err != nil {
		return fmt.Errorf("state: %v", err)
	}

	secUntilEmpty, err := b.dev.Properties.Get("org.freedesktop.UPower", "TimeToEmpty")
	if err != nil {
		return fmt.Errorf("until empty: %v", err)
	}
	untilEmpty64, ok := secUntilEmpty.(int64)
	if !ok {
		return fmt.Errorf("until empty: not an int64")
	}
	untilEmpty := time.Duration(untilEmpty64) * time.Second
	m.UntilEmpty = &untilEmpty

	secUntilFull, err := b.dev.Properties.Get("org.freedesktop.UPower", "TimeToFull")
	if err != nil {
		return fmt.Errorf("until full: %v", err)
	}
	untilFull64, ok := secUntilFull.(int64)
	if !ok {
		return fmt.Errorf("until full: not an int64")
	}
	untilFull := time.Duration(untilFull64) * time.Second
	m.UntilFull = &untilFull

	percent, err := b.dev.Charge()
	if err != nil {
		return fmt.Errorf("charge: %v", err)
	}
	m.Fraction = percent / 100

	b.mut.Lock()
	b.metrics = &m
	b.mut.Unlock()

	return nil
}

func (b *BatteryProfiler) Metrics() *BatteryMetrics {
	resp := make(chan *BatteryMetrics, 1)
	b.ping <- resp
	m := <-resp
	return m
}

type BatteryMetrics struct {
	Fraction   float64
	State      upower.DeviceState
	UntilEmpty *time.Duration
	UntilFull  *time.Duration
}

// TODO:
// Modify to return a possible error.
type MetricFormatter interface {
	// Format renders m in a human digestable way, generally highlighting one
	// metric in particular.
	Format(m *BatteryMetrics) string
}

type MaxMetricFormatter interface {
	// MaxFormattedWidth returns a string that approximates the width of the
	// formatted output.
	MaxFormattedWidth() string
}

type MetricFormatFunc func(*BatteryMetrics) string

func (fn MetricFormatFunc) Format(m *BatteryMetrics) string {
	return fn(m)
}

var batteryMetricTemplateFuncs = template.FuncMap{
	"dur": func(d time.Duration) string {
		return cleanDurationString(d)
	},
	"durShort": func(d time.Duration) string {
		return shortDurationString(d)
	},
	"percent": func(fraction float64) string {
		return fmt.Sprintf("%d%%", roundBiasLow(fraction*100))
	},
}

type templateMetricFormatter struct {
	t   *template.Template
	buf bytes.Buffer
}

func newTemplateMetricFormatter(s string) (*templateMetricFormatter, error) {
	t, err := template.New("batterymetric").Funcs(batteryMetricTemplateFuncs).Parse(s)
	if err != nil {
		return nil, err
	}
	f := &templateMetricFormatter{t: t}
	return f, nil
}

func (f *templateMetricFormatter) Format(m *BatteryMetrics) string {
	f.buf.Truncate(0)
	remaining := m.UntilEmpty
	if m.State == upower.Charging {
		remaining = m.UntilFull
	}
	err := f.t.Execute(&f.buf, map[string]interface{}{
		"fraction":   m.Fraction,
		"state":      m.State.String(),
		"remaining":  remaining,
		"untilFull":  m.UntilFull,
		"untilEmpty": m.UntilEmpty,
	})
	if err != nil {
		log.Printf("template: %v", err)
	}
	return strings.Join(strings.Fields(strings.TrimSpace(f.buf.String())), " ")
}

// BUG:
// Template errors can not be intercepted.  They are only logged.
func BatteryMetricTemplate(s string) (MetricFormatter, error) {
	return newTemplateMetricFormatter(s)
}

func SimpleMetricsFormat(m *BatteryMetrics) string {
	return fmt.Sprintf("%2d%% %s", roundBiasLow(m.Fraction*100), cleanDurationString(*m.UntilEmpty))
}

func BatteryPercent(m *BatteryMetrics) string {
	return fmt.Sprintf("%d%%", roundBiasLow(m.Fraction*100))
}

func BatteryRemaining(m *BatteryMetrics) string {
	switch m.State {
	case upower.Charging:
		return cleanDurationString(*m.UntilFull) + " left"
	case upower.Discharging:
		return cleanDurationString(*m.UntilEmpty) + " left"
	case upower.Full:
		return "Full"
	case upower.Empty:
		return "Empty"
	default:
		return "???"
	}
}

func shortDurationString(d time.Duration) string {
	d = (d / time.Minute) * time.Minute
	if d == 0 {
		return "0m"
	}
	s := d.String()
	i := strings.IndexAny(s, "hm")
	if i < 0 {
		return s
	}
	return s[:i+1]
}

func cleanDurationString(d time.Duration) string {
	d = (d / time.Minute) * time.Minute
	if d == 0 {
		return "0m"
	}
	s := d.String()
	s = strings.Replace(s, "m0s", "m", 1)
	s = strings.Replace(s, "h0m", "h", 1)
	return s
}

func roundBiasLow(x float64) int {
	return int(math.Ceil(x - 0.5))
}

func BatteryState(m *BatteryMetrics) string {
	return m.State.String()
}

func RotateMetricsFormat(interval time.Duration, c chan<- MetricFormatter, f ...MetricFormatter) {
	tick := time.NewTicker(interval)
	defer tick.Stop()
	var i int
	first := make(chan struct{})
	close(first)
	_c := c
	for {
		select {
		case _c <- f[i]:
			_c = nil
		case <-tick.C:
			i = (i + 1) % len(f)
			_c = c
		}
	}
}
