/*
Command dockapp-cpu is a simple, customizable cpu utilization indicator dockapp
for Openbox.

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
	"strings"
	"time"

	"github.com/BurntSushi/xgbutil"
	"github.com/bmatsuo/sandbox/dockapp-go/dockapp"
	"github.com/bmatsuo/sandbox/dockapp-go/geometry"
)

func main() {
	window := geometry.Flag("window.geometry", image.Rect(0, 0, 100, 20), "window geometry in pixels")
	ignore := flag.String("ignore", "", "comma separated list of cpus to ignore")
	borderThickness := flag.Int("border", 1, "battery border thickness in pixels")
	flag.Parse()

	p, err := Poll(time.Second)
	if err != nil {
		log.Fatal(err)
	}
	delta := Delta(p.C)

	// configure the application window layout
	layout := &AppLayout{
		rect:      *window,
		thickness: *borderThickness,
	}
	if *ignore != "" {
		layout.Ignore = strings.Split(*ignore, ",")
	}

	app := NewApp(layout)

	// Connect to the x server and create a dockapp window for the process.
	X, err := xgbutil.NewConn()
	if err != nil {
		log.Fatal(err)
	}
	dockapp, err := dockapp.New(X, *window)
	if err != nil {
		log.Fatal(err)
	}
	defer dockapp.Destroy()

	// begin the main draw loop. the draw loop receives updates in the form of
	// new battery metrics and formatters.  The event loop will exit if the
	// draw loop ever terminates.
	go RunApp(dockapp, app, delta)

	// finally map the window and start the main event loop
	dockapp.Main()
}

func RunApp(dockapp *dockapp.DockApp, app *App, delta <-chan []*Time) {
	defer dockapp.Quit()

	var cpuNamesOld []string
	for {
		var times []*Time
		select {
		case times = <-delta:
		}

		var _times []*Time
		if len(app.Layout.Ignore) > 0 {
			for _, t := range times {
				for _, name := range app.Layout.Ignore {
					if t.Name == name {
						continue
					}
					_times = append(_times, t)
				}
			}
			times = _times
		}

		var cpuNames []string
		for _, t := range times {
			cpuNames = append(cpuNames, t.Name)
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
		err := app.Draw(dockapp.Canvas(), times)
		if err != nil {
			log.Panic(err)
		}
		dockapp.FlushImage()
	}
}

type AppLayout struct {
	rect      image.Rectangle
	thickness int
	Ignore    []string
}

type App struct {
	drawCPU func(draw.Image, image.Rectangle, *Time) error
	Layout  *AppLayout
}

func NewApp(layout *AppLayout) *App {
	app := &App{
		Layout: layout,
	}
	return app
}

var white = image.NewUniform(color.White)
var black = image.NewUniform(color.Black)
var transparent = image.NewUniform(color.Transparent)
var opaque = image.NewUniform(color.Opaque)

func (app *App) Draw(img draw.Image, times []*Time) error {
	rect := app.Layout.rect
	draw.Draw(img, rect, image.White, image.Point{}, draw.Over)

	cpuDx := rect.Dx() / len(times)
	ptIncr := image.Point{X: cpuDx}
	ptDelta := image.Point{}
	for _, t := range times {
		irect := image.Rectangle{
			Min: rect.Min.Add(ptDelta),
			Max: rect.Max.Add(ptDelta).Add(ptIncr),
		}

		drawCPU := DrawCPU
		if app.drawCPU != nil {
			drawCPU = app.drawCPU
		}
		err := drawCPU(img, irect, t)
		if err != nil {
			return err
		}

		ptDelta = ptDelta.Add(ptIncr)
	}
	return nil
}

func DrawCPU(img draw.Image, rect image.Rectangle, cpuTime *Time) error {
	utilized := UtilFrac(cpuTime)
	utilizedHeight := int(float64(rect.Dy()) * utilized)
	utilizedRect := image.Rectangle{
		Min: rect.Min.Add(image.Point{Y: rect.Dy() - utilizedHeight}),
		Max: rect.Max,
	}
	draw.Draw(img, utilizedRect, image.Black, image.Point{}, draw.Over)
	return nil
}
