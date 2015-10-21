package creeperguage

import (
	"fmt"
	"log"
	"time"

	"github.com/TheCreeper/go-upower"
	"github.com/TheCreeper/go-upower/device"
	"github.com/bmatsuo/dockapp-go/cmd/dockapp-battery/battery"
	"github.com/godbus/dbus"
)

// CreeperBatteryGuage is a BatteryGuage implementation that uses github.com/TheCreeper/go-upower
type CreeperBatteryGuage struct {
	dev dbus.ObjectPath
	sig chan *dbus.Signal
}

// NewCreeperBatteryGuage detects batteries on the system and returs a
// CreeperBatteryGuage that reads its metrics.
func NewCreeperBatteryGuage() (*CreeperBatteryGuage, error) {
	batts, err := getBatteries()
	if err != nil {
		return nil, err
	}
	if len(batts) == 0 {
		return nil, fmt.Errorf("no batteries")
	}

	g := &CreeperBatteryGuage{
		dev: batts[0],
	}

	return g, nil
}

// BatteryMetrics implements the BatteryGuage interface.
func (g *CreeperBatteryGuage) BatteryMetrics() (*battery.Metrics, error) {
	state, err := propUint32(g.dev, "org.freedesktop.UPower.State")
	if err != nil {
		return nil, fmt.Errorf("state: %v", err)
	}
	percent, err := propFloat64(g.dev, "org.freedesktop.UPower.Percentage")
	if err != nil {
		return nil, fmt.Errorf("charge: %v", err)
	}
	untilEmpty, err := propDurSec(g.dev, "org.freedesktop.UPower.TimeToEmpty")
	if err != nil {
		return nil, fmt.Errorf("until empty: %v", err)
	}
	untilFull, err := propDurSec(g.dev, "org.freedesktop.UPower.TimeToFull")
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

// BatteryStateChange implements the BatteryStateNotifier interface.
func (g *CreeperBatteryGuage) BatteryStateChange(notf chan<- struct{}) (stop func()) {
	_done := make(chan struct{})

	if g.sig == nil {
		sig, err := device.SignalChanged()
		if err != nil {
			close(notf)
			return func() {}
		}
		g.sig = sig
	}

	go func() {
		if !g.reconnect() {
			return
		}
		var relay chan<- struct{}
		for {
			select {
			case s, ok := <-g.sig:
				if !ok {
					log.Printf("upower: state channel closed")

					if g.reconnect() {
						continue
					}
					return
				}
				if s.Path != g.dev {
					continue
				}
				relay = notf
			case relay <- struct{}{}:
				relay = nil
			case <-_done:
				return
			}
		}
	}()

	return func() { close(_done) }
}

func (g *CreeperBatteryGuage) reconnect() (ok bool) {
	var err error
	g.sig, err = device.SignalChanged()
	if err != nil {
		log.Printf("upower: %v", err)
		return false
	}
	return true
}

func getBatteries() ([]dbus.ObjectPath, error) {
	devs, err := upower.EnumerateDevices()
	if err != nil {
		return nil, err
	}
	var batts []dbus.ObjectPath
	for _, dev := range devs {
		if isBattery(dev) {
			batts = append(batts, dev)
		}
	}
	return batts, nil
}

func isBattery(path dbus.ObjectPath) bool {
	log.Print(path)
	x, err := propUint32(path, "org.freedesktop.UPower.Type")
	if err != nil {
		log.Print(err)
		return false
	}
	return x == device.Battery
}

func propFloat64(path dbus.ObjectPath, prop string) (float64, error) {
	v, err := device.GetProperty(path, prop)
	if err != nil {
		return 0, err
	}
	x, ok := v.Value().(float64)
	if !ok {
		return 0, fmt.Errorf("not uint32")
	}
	return x, nil
}

func propUint32(path dbus.ObjectPath, prop string) (uint32, error) {
	v, err := device.GetProperty(path, prop)
	if err != nil {
		return 0, err
	}
	x, ok := v.Value().(uint32)
	if !ok {
		return 0, fmt.Errorf("not uint32")
	}
	return x, nil
}

func propDurSec(path dbus.ObjectPath, prop string) (time.Duration, error) {
	x, err := propInt64(path, prop)
	if err != nil {
		return 0, err
	}
	dur := time.Duration(x) * time.Second
	return dur, nil
}

func propInt64(path dbus.ObjectPath, prop string) (int64, error) {
	v, err := device.GetProperty(path, prop)
	if err != nil {
		return 0, err
	}
	x, ok := v.Value().(int64)
	if !ok {
		return 0, fmt.Errorf("not int64")
	}
	return x, nil
}
