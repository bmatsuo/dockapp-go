package battery

import (
	"bytes"
	"fmt"
	"log"
	"math"
	"strings"
	"text/template"
	"time"
)

//go:generate stringer -type=State

// State is the state the battery is in.  The values correspond with
// upower integer values.
type State int

// State values.
const (
	Charging State = 1 + iota
	Discharging
	Empty
	FullyCharged
	PendingCharge
	PendingDischarge
)

// Metrics describes the set state of the computer's battery.
type Metrics struct {
	Fraction   float64
	State      State
	UntilEmpty *time.Duration
	UntilFull  *time.Duration
}

// MetricFormatter returns a readable string from Metrics.
// TODO:
// Modify to return a possible error.
type MetricFormatter interface {
	// Format renders m in a human digestable way, generally highlighting one
	// metric in particular.
	Format(m *Metrics) string
}

// MaxMetricFormatter helps layout engines determine the size required to
// graphically render Metrics.
type MaxMetricFormatter interface {
	// MaxFormattedWidth returns a string that approximates the width of the
	// formatted output.
	MaxFormattedWidth() string
}

// MetricFormatFunc is a function that implements the MetricFormatter interface.
type MetricFormatFunc func(*Metrics) string

// Format implements the MetricFormatter interface.
func (fn MetricFormatFunc) Format(m *Metrics) string {
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

func (f *templateMetricFormatter) Format(m *Metrics) string {
	f.buf.Truncate(0)
	remaining := m.UntilEmpty
	if m.State == Charging {
		remaining = m.UntilFull
	}
	err := f.t.Execute(&f.buf, map[string]interface{}{
		"fraction":   m.Fraction,
		"state":      m.State,
		"remaining":  remaining,
		"untilFull":  m.UntilFull,
		"untilEmpty": m.UntilEmpty,
	})
	if err != nil {
		log.Printf("template: %v", err)
	}
	return strings.Join(strings.Fields(strings.TrimSpace(f.buf.String())), " ")
}

// FormatMetricTemplate renders Metrics using the template string s.
//
// BUG:
// Template errors can not be intercepted.  They are only logged.
func FormatMetricTemplate(s string) (MetricFormatter, error) {
	return newTemplateMetricFormatter(s)
}

// SimpleMetricsFormat is a simple MetricsFormatter.
func SimpleMetricsFormat(m *Metrics) string {
	return fmt.Sprintf("%2d%% %s", roundBiasLow(m.Fraction*100), cleanDurationString(*m.UntilEmpty))
}

// FormatPercent renders the battery level as an integral percentage.
func FormatPercent(m *Metrics) string {
	return fmt.Sprintf("%d%%", roundBiasLow(m.Fraction*100))
}

// FormatRemaining returns a human readable string describing the time until
// the battery is empty/full.  If the battery is empty then "Empty" is
// returned.  If the battery is full then "Full" is returned.
func FormatRemaining(m *Metrics) string {
	switch m.State {
	case Charging:
		return cleanDurationString(*m.UntilFull) + " left"
	case Discharging:
		return cleanDurationString(*m.UntilEmpty) + " left"
	case FullyCharged:
		return "Full"
	case Empty:
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

// FormatState returns the string representation of a battery's state.
func FormatState(m *Metrics) string {
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
