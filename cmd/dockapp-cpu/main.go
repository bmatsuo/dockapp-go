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

	app := NewApp()

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

	// begin the main draw loop. the draw loop receives updates in the form of
	// new battery metrics and formatters.  The event loop will exit if the
	// draw loop ever terminates.
	go RunApp(dockapp, app, deltaCPU)

	go func() {
		defer log.Print("!!!")
		defer func() {
			go func() {
				time.Sleep(5 * time.Second)
				panic("bababa")
			}()
			dockapp.Quit()
		}()
		defer log.Print("!!")
		defer dockapp.Destroy()
		for {
			select {
			case s := <-sig:
				signal.Stop(sig)

				log.Printf("signal received: %s", s)

				poll.Stop()
			case <-app.Done():
				return
			}
		}
	}()

	// map the window and start the main event loop
	dockapp.Main()
}

func RunApp(dockapp *dockapp.DockApp, app *App, delta <-chan []CPU) {
	defer close(app.done)

	img := dockapp.Canvas()
	app.Draw(img, nil)
	dockapp.FlushImage()

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
		app.Draw(dockapp.Canvas(), cpus)
		dockapp.FlushImage()
	}
}

type App struct {
	done       chan struct{}
	Background image.Image
	Renderer   Renderer
}

func NewApp() *App {
	app := &App{
		done: make(chan struct{}),
	}
	return app
}

func (app *App) Done() <-chan struct{} {
	return app.done
}

func (app *App) renderCPU(img draw.Image, cpu CPU) {
	r := DefaultRenderer
	if app.Renderer != nil {
		r = app.Renderer
	}
	r.RenderCPU(img, cpu)
}

func (app *App) Draw(img draw.Image, cpus []CPU) {
	rect := img.Bounds()
	bg := app.Background
	if bg == nil {
		bg = image.Black
	}
	draw.Draw(img, rect, bg, bg.Bounds().Min, draw.Over)

	if len(cpus) == 0 {
		return
	}

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
		subimg := SubImage(img, irect)
		app.renderCPU(subimg, cpu)

		ptDelta = ptDelta.Add(ptIncr)
	}
}

type Renderer interface {
	RenderCPU(draw.Image, CPU)
}

type Border struct {
	Size     int
	Color    color.Color
	Renderer Renderer
}

func (b *Border) RenderCPU(img draw.Image, cpu CPU) {
	rect := img.Bounds()
	interior := geometry.Contract(rect, image.Pt(b.Size, b.Size))
	mask := MaskInside(interior)
	draw.DrawMask(img, rect, image.NewUniform(b.Color), image.ZP, mask, rect.Min, draw.Over)
	sub := SubImage(img, interior)
	b.Renderer.RenderCPU(sub, cpu)
}

type BackgroundRenderer struct {
	Color    color.Color
	Renderer Renderer
}

func (bg *BackgroundRenderer) RenderCPU(img draw.Image, cpu CPU) {
	draw.Draw(img, img.Bounds(), image.NewUniform(bg.Color), image.ZP, draw.Over)
	bg.Renderer.RenderCPU(img, cpu)
}

type FractionRenderer struct {
	Horizontal bool
	Renderer   Renderer
}

func (frac *FractionRenderer) RenderCPU(img draw.Image, cpu CPU) {
	rect := img.Bounds()

	utilized := cpu.FracUtil()
	utilizedHeight := int(float64(rect.Dy()) * utilized)
	yoffset := rect.Dy() - utilizedHeight
	rect.Min = rect.Min.Add(image.Pt(0, yoffset))
	img = SubImage(img, rect)

	frac.Renderer.RenderCPU(img, cpu)
}

type SimpleGradient struct {
	C1, C2 color.Color
}

func (grad *SimpleGradient) RenderCPU(img draw.Image, cpu CPU) {

	r1, g1, b1, a1 := grad.C1.RGBA()
	r2, g2, b2, a2 := grad.C2.RGBA()

	const M = 0xFFFF
	m := uint32(cpu.FracUtil() * float64(M))
	// The resultant red value is a blend of dstr and srcr, and ranges in [0, M].
	// The calculation for green, blue and alpha is similar.
	r := (r1*(M-m) + r2*m) / M
	g := (g1*(M-m) + g2*m) / M
	b := (b1*(M-m) + b2*m) / M
	a := (a1*(M-m) + a2*m) / M

	utilColor := color.RGBA64{
		R: uint16(r),
		G: uint16(g),
		B: uint16(b),
		A: uint16(a),
	}

	draw.Draw(img, img.Bounds(), image.NewUniform(utilColor), image.ZP, draw.Over)
}

var DefaultRenderer Renderer = &BackgroundRenderer{
	Color: color.White,
	Renderer: &Border{
		Size:  1,
		Color: color.Black,
		Renderer: &FractionRenderer{
			Renderer: &SimpleGradient{
				C1: color.RGBA{G: 0xff, A: 0xff},
				C2: color.RGBA{R: 0xff, A: 0xff},
			},
		},
	},
}

// SubImage produces a subimage of img as seen through r.  Attempts to draw
// outside of r (or img) have no effect.
func SubImage(img draw.Image, r image.Rectangle) draw.Image {
	r = img.Bounds().Intersect(r)
	return &drawSubImage{img, r}
}

type drawSubImage struct {
	img draw.Image
	r   image.Rectangle
}

func (img *drawSubImage) ColorModel() color.Model {
	return img.img.ColorModel()
}

func (img *drawSubImage) Bounds() image.Rectangle {
	return img.r
}

func (img *drawSubImage) At(x, y int) color.Color {
	if image.Pt(x, y).In(img.r) {
		return img.img.At(x, y)
	}
	panic("color at out of bounds index")
}

func (img *drawSubImage) Set(x, y int, c color.Color) {
	if image.Pt(x, y).In(img.r) {
		img.img.Set(x, y, c)
	}
}

type Mask struct {
	image.Image
	R      image.Rectangle
	Inside bool
}

func MaskInside(r image.Rectangle) *Mask {
	return &Mask{image.Opaque, r, true}
}

func MaskOutside(r image.Rectangle) *Mask {
	return &Mask{image.Opaque, r, false}
}

func (m *Mask) At(x, y int) color.Color {
	inR := image.Pt(x, y).In(m.R)
	if inR && m.Inside {
		return color.Transparent
	}
	if !inR && !m.Inside {
		return color.Transparent
	}
	return m.Image.At(x, y)
}
