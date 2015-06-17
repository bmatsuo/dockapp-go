/*
Command dockapp-cpu is a simple, customizable cpu utilization indicator dockapp
for Openbox.  CPU statistics from /proc/stat are displayed as CPU utilization
(time spent non-idle).

Examples

A minimal window with custom geometry:

	dockapp-cpu -window.geometry=40x20

Help

For command usage and other help run dockapp-battery with the -h flag.
*/
package main

import (
	"flag"
	"image"
	"image/color"
	"image/draw"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/BurntSushi/xgbutil"
	"github.com/bmatsuo/sandbox/dockapp-go/dockapp"
	"github.com/bmatsuo/sandbox/dockapp-go/geometry"
)

func main() {
	defer func() {
		if e := recover(); e != nil {
			panic(e)
		}
		panic("show me the stacks")
	}()
	window := geometry.Flag("window.geometry", image.Rect(0, 0, 100, 20), "window geometry in pixels")
	ignore := flag.String("ignore", "", "comma separated list of cpus to ignore")
	borderThickness := flag.Int("border", 1, "battery border thickness in pixels")
	flag.Parse()

	poll, err := Poll(time.Second)
	if err != nil {
		log.Fatal(err)
	}
	delta := Delta(poll.C)
	deltaCPU := TimeToCPU(delta)
	if *ignore != "" {
		ignores := strings.Split(*ignore, ",")
		deltaCPU = FilterCPU(deltaCPU, ignores)
	}

	// configure the application window layout
	layout := &AppLayout{
		rect:   *window,
		Border: *borderThickness,
	}

	app := NewApp(layout)

	// Connect to the x server and create a dockapp window for the process.
	X, err := xgbutil.NewConn()
	if err != nil {
		log.Fatal(err)
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)

	dockapp, err := dockapp.New(X, *window)
	if err != nil {
		log.Fatal(err)
	}
	defer dockapp.Destroy()
	defer dockapp.Quit()
	// map the window and start the main event loop
	go dockapp.Main()

	// begin the main draw loop. the draw loop receives updates in the form of
	// new battery metrics and formatters.  The event loop will exit if the
	// draw loop ever terminates.
	go RunApp(dockapp, app, deltaCPU)

	var timeout <-chan time.Time
	for {
		select {
		case s := <-sig:
			signal.Stop(sig)

			log.Printf("signal received: %s", s)

			poll.Stop()
			timeout = time.After(time.Second)
		case <-timeout:
			panic("timeout")
		case <-app.Done():
			return
		}
	}
}

func RunApp(dockapp *dockapp.DockApp, app *App, delta <-chan []CPU) {
	defer close(app.done)

	var cpus []CPU
	var ok bool
	var cpuNamesOld []string
	for {
		select {
		case cpus, ok = <-delta:
			if !ok {
				return
			}
		}

		var cpuNames []string
		for _, t := range cpus {
			cpuNames = append(cpuNames, t.Name())
		}
		if len(cpuNames) != len(cpuNamesOld) {
			cpuNamesOld = cpuNames
			log.Printf("cpus: %q", cpuNames)
		} else {
			for i, name := range cpuNamesOld {
				if name != cpuNames[i] {
					cpuNamesOld = cpuNames
					log.Printf("cpus: %q", cpuNames)
				}
			}
		}

		// draw the widget to the screen.
		err := app.Draw(dockapp.Canvas(), cpus)
		if err != nil {
			log.Panic(err)
		}
		dockapp.FlushImage()
	}
}

type AppLayout struct {
	rect   image.Rectangle
	Border int
}

type App struct {
	done    chan struct{}
	DrawCPU func(draw.Image, image.Rectangle, CPU) error
	Layout  *AppLayout
}

func NewApp(layout *AppLayout) *App {
	app := &App{
		done:   make(chan struct{}),
		Layout: layout,
	}
	return app
}

func (app *App) Done() <-chan struct{} {
	return app.done
}

var white = image.NewUniform(color.White)
var black = image.NewUniform(color.Black)
var transparent = image.NewUniform(color.Transparent)
var opaque = image.NewUniform(color.Opaque)

func (app *App) drawCPU(img draw.Image, rect image.Rectangle, cpu CPU) error {
	if app.DrawCPU != nil {
		return app.DrawCPU(img, rect, cpu)
	}
	return DrawCPU(img, rect, app.Layout.Border, cpu)
}

func (app *App) Draw(img draw.Image, cpus []CPU) error {
	rect := app.Layout.rect
	draw.Draw(img, rect, image.Black, image.Point{}, draw.Over)

	cpuDx := rect.Dx() / len(cpus)
	ptIncr := image.Point{X: cpuDx}
	ptDelta := image.Point{}
	rectDx := image.Rectangle{
		Min: rect.Min,
		Max: rect.Max,
	}
	rectDx.Max.X = rect.Min.X + cpuDx
	for _, cpu := range cpus {
		irect := image.Rectangle{
			Min: rectDx.Min.Add(ptDelta),
			Max: rectDx.Max.Add(ptDelta),
		}
		contract := image.Pt(app.Layout.Border, app.Layout.Border)
		irect = geometry.Contract(irect, contract)

		err := app.drawCPU(img, irect, cpu)
		if err != nil {
			return err
		}

		ptDelta = ptDelta.Add(ptIncr)
	}
	return nil
}

func DrawCPU(img draw.Image, rect image.Rectangle, border int, cpu CPU) error {
	draw.Draw(img, rect, image.White, image.Point{}, draw.Over)

	utilizedHeight := int(float64(rect.Dy()) * cpu.FracUtil())
	utilizedRect := image.Rectangle{
		Min: rect.Min.Add(image.Point{Y: rect.Dy() - utilizedHeight}),
		Max: rect.Max,
	}
	draw.Draw(img, utilizedRect, image.NewUniform(color.RGBA{0xff, 0, 0, 0xff}), image.Point{}, draw.Over)

	return nil
}
