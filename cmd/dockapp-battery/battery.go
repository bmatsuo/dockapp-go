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

// BatteryGuage is an interface that can derive metrics for the computer's
// battery.
type BatteryGuage interface {
	BatteryMetrics() (*BatteryMetrics, error)
}

// BatteryStateNotifier complements a BatteryGuage by sending over
// notifications when the battery "connected" state has changed.
type BatteryStateNotifier interface {
	BatteryStateChange(notifications chan<- struct{}) (stop func())
}

// BatteryProfiler is a BatteryGuage that periodically polls an underlying
// BatteryGuage.
type BatteryProfiler struct {
	g      BatteryGuage
	change chan struct{}
	stop   chan struct{}

	mut     sync.RWMutex
	metrics *BatteryMetrics
}

// NewBatteryProfiler returns a new BatteryProfiler that periodically polls g.
func NewBatteryProfiler(g BatteryGuage) *BatteryProfiler {
	b := new(BatteryProfiler)
	b.stop = make(chan struct{})
	b.g = g
	return b
}

// Start begins polling the underlying BatteryGuage at the specified interval
// and sends BatteryMetrics over c.
func (b *BatteryProfiler) Start(interval time.Duration, c chan<- *BatteryMetrics) {
	watchStop := b.watchBatteryState()
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

func (b *BatteryProfiler) watchBatteryState() func() {
	if notf, ok := b.g.(BatteryStateNotifier); ok {
		b.change = make(chan struct{})
		return notf.BatteryStateChange(b.change)
	}
	return func() {} // noop
}

// Stop prevents future poll events.
func (b *BatteryProfiler) Stop() {
	close(b.stop)
}

func (b *BatteryProfiler) refreshMetrics() error {
	m, err := b.g.BatteryMetrics()
	if err != nil {
		return err
	}
	b.mut.Lock()
	b.metrics = m
	b.mut.Unlock()
	return nil
}

func (b *BatteryProfiler) batteryMetrics() *BatteryMetrics {
	b.mut.RLock()
	m := b.metrics
	b.mut.RUnlock()
	return m
}

// BatteryMetrics implements the BatteryGuage interface and returns cached
// metrics from the underlying BatteryGuage.
func (b *BatteryProfiler) BatteryMetrics() (*BatteryMetrics, error) {
	return b.batteryMetrics(), nil
}

// GobarBatteryGuage uses the gobar upower interface to retrieve battery
// metrics.
type GobarBatteryGuage struct {
	conn   *dbus.Connection
	upower *upower.UPower
	dev    *upower.Device
}

// NewGobarBatteryGuage connects to the dbus system bus
func NewGobarBatteryGuage() (*GobarBatteryGuage, error) {
	g := &GobarBatteryGuage{}
	var err error
	g.conn, err = dbus.Connect(dbus.SystemBus)
	if err != nil {
		return nil, fmt.Errorf("connect: %v", err)
	}
	g.upower = upower.New(g.conn)
	g.dev, err = g.upower.GetBattery()
	if err != nil {
		return nil, fmt.Errorf("battery: %v", err)
	}
	return g, nil
}

// BatteryMetrics reads and return metrics from the upower interface.
func (g *GobarBatteryGuage) BatteryMetrics() (*BatteryMetrics, error) {
	state, err := g.dev.State()
	if err != nil {
		return nil, fmt.Errorf("state: %v", err)
	}
	percent, err := g.dev.Charge()
	if err != nil {
		return nil, fmt.Errorf("charge: %v", err)
	}
	untilEmpty, err := g.propDurSec("org.freedesktop.UPower", "TimeToEmpty")
	if err != nil {
		return nil, fmt.Errorf("until empty: %v", err)
	}
	untilFull, err := g.propDurSec("org.freedesktop.UPower", "TimeToFull")
	if err != nil {
		return nil, fmt.Errorf("until full: %v", err)
	}

	m := &BatteryMetrics{
		State:      state,
		Fraction:   percent / 100,
		UntilEmpty: &untilEmpty,
		UntilFull:  &untilFull,
	}

	return m, nil
}

func (g *GobarBatteryGuage) propDurSec(i, s string) (time.Duration, error) {
	x, err := g.propInt64(i, s)
	if err != nil {
		return 0, err
	}
	dur := time.Duration(x) * time.Second
	return dur, nil
}

func (g *GobarBatteryGuage) propInt64(i, s string) (int64, error) {
	v, err := g.dev.Properties.Get(i, s)
	if err != nil {
		return 0, err
	}
	x, ok := v.(int64)
	if !ok {
		return 0, fmt.Errorf("not an int64")
	}
	return x, nil
}

// BatteryStateChange implements the BatteryStateNotifier interface.
func (g *GobarBatteryGuage) BatteryStateChange(notf chan<- struct{}) (stop func()) {
	_done := make(chan struct{})
	g.dev.Connect(func(*upower.Device) {
		select {
		case <-_done:
		case notf <- struct{}{}:
		}
	})
	return func() { close(_done) }
}

// BatteryMetrics describes the set state of the computer's battery.
type BatteryMetrics struct {
	Fraction   float64
	State      upower.DeviceState
	UntilEmpty *time.Duration
	UntilFull  *time.Duration
}

// MetricFormatter returns a readable string from BatteryMetrics.
// TODO:
// Modify to return a possible error.
type MetricFormatter interface {
	// Format renders m in a human digestable way, generally highlighting one
	// metric in particular.
	Format(m *BatteryMetrics) string
}

// MaxMetricFormatter helps layout engines determine the size required to
// graphically render BatteryMetrics.
type MaxMetricFormatter interface {
	// MaxFormattedWidth returns a string that approximates the width of the
	// formatted output.
	MaxFormattedWidth() string
}

// MetricFormatFunc is a function that implements the MetricFormatter interface.
type MetricFormatFunc func(*BatteryMetrics) string

// Format implements the MetricFormatter interface.
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

// BatteryMetricTemplate renders BatteryMetrics using the template string s.
//
// BUG:
// Template errors can not be intercepted.  They are only logged.
func BatteryMetricTemplate(s string) (MetricFormatter, error) {
	return newTemplateMetricFormatter(s)
}

// SimpleMetricsFormat is a simple MetricsFormatter.
func SimpleMetricsFormat(m *BatteryMetrics) string {
	return fmt.Sprintf("%2d%% %s", roundBiasLow(m.Fraction*100), cleanDurationString(*m.UntilEmpty))
}

// BatteryPercent renders the battery level as an integral percentage.
func BatteryPercent(m *BatteryMetrics) string {
	return fmt.Sprintf("%d%%", roundBiasLow(m.Fraction*100))
}

// BatteryRemaining returns a human readable string describing the time until
// the battery is empty/full.  If the battery is empty then "Empty" is
// returned.  If the battery is full then "Full" is returned.
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

// roundBiasLow rounds x to an integer with a bias toward -Inf.
func roundBiasLow(x float64) int {
	return int(math.Ceil(x - 0.5))
}

// BatteryState returns the string representation of a battery's state.
func BatteryState(m *BatteryMetrics) string {
	return m.State.String()
}

// RotateMetricsFormat sends an f over c every interval.
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
