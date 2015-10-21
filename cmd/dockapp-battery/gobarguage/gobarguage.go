package gobarguage

import (
	"fmt"
	"time"

	"github.com/AmandaCameron/gobar/utils/dbus/upower"
	"github.com/bmatsuo/dockapp-go/cmd/dockapp-battery/battery"
	"launchpad.net/~jamesh/go-dbus/trunk"
)

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
func (g *GobarBatteryGuage) BatteryMetrics() (*battery.Metrics, error) {
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

	m := &battery.Metrics{
		State:      battery.State(state),
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
