/*
Command dockapp-battery is a simple, customizable battery indicator dockapp for
Openbox.

Text templates

Arguments to dockapp-battery are interpretted as templates which render metric
information into a human consumable format.  Each template is displayed for a
configurable amount of time.

	dockapp-battery -text.interval=5s '{{percent .fraction}}' '{{.state}}'

The above command alternates between showing the battery charge percentage and
state for 5 seconds each.

Templates are written using the Go text/template language.

	http://godoc.org/text/template

Templates are evaluated with the following variables available.

	fraction    The fraction of total capacity available as a floating point number
	percent		The fraction of total capacity as an integer perent (e.g. "85%")
	state       The state of the battery (e.g. "Charging", "Discharging", "Full", ...)
	remaining   When charging the time until full, when discharging the time until empty
	untilFull   The time until the battery is full
	untilEmpty  The time until the battery is empty

Several functions are defined for templates to facilitate rendering of
durations.

	dur       Render a duration with minute precision (e.g. "4h3m" instead of "4h3m15s")
	durShort  Render a duration with variable precision (e.g. "4h" instead of "4h3m")

Fonts

Dockapp-battery attempts to locate fonts based on simple names like
"DejaVuSans-Bold" or "Ubuntu-B". As an alternative any truetype font (.ttf)
file can be specified through an absolute path.

	dockapp-battery -text.font="$PWD/myfont.ttf"

BUG(bmatsuo):
Font detection is flakey and done with globs.  Ideally a utility like
fontconfig (fc-*) would be great but from what I've seen it's matching ability
is pretty bad.

Geometry

There are three areas within the dockapp: the window, the battery graphic
bounding box, and the text bounding box.  These geometries are given using
standard <w>x<h>[<+dx><+dy>] notation.

The rectangles may overlap. Content is drawn in the order: background, battery,
text. So text will always render on top of the battery, which always renders on
top of the background.

	dockapp-battery -window.geometry=40x20 -battery.geometry=38x18+1+1 -text.geometry=38x18+1+1 '{{percent .fraction}}'

The above command renders the dockapp in a compact 40x20 rectangle with the
percentage overlaid on the battery graphic.

Examples

A minimal window that displays percent charged and the remaining time as a
short string.

	dockapp-battery \
    	-window.geometry  '40x20' \
    	-battery.geometry '38x18+1+1' \
    	-text.geometry    '38x18+1+1' \
    	'{{percent .fraction}}' \
    	'{{durShort .remaining}}'

The template language can be used to combine metrics for more expressive
messages.

	dockapp-battery \
		-window.geometry '150x20' \
		-text.geometry   '130x20+20+0' \
		'{{if eq .state "Full"}}
		Battery full
		{{else if eq .state "Charging"}}
		{{durShort .remaining}} until full
		{{else}}{{durShort .remaining}} until empty
		{{end}}'

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
	"math"
	"time"

	"github.com/BurntSushi/xgbutil"
	"github.com/bmatsuo/dockapp-go/cmd/dockapp-battery/battery"
	"github.com/bmatsuo/dockapp-go/cmd/dockapp-battery/creeperguage"
	"github.com/bmatsuo/dockapp-go/dockapp"
	"github.com/bmatsuo/dockapp-go/geometry"
	"github.com/golang/freetype"
	"github.com/golang/freetype/truetype"
	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

var defaultFormatters = []battery.MetricFormatter{
	battery.MetricFormatFunc(battery.FormatState),
	battery.MetricFormatFunc(battery.FormatPercent),
	battery.MetricFormatFunc(battery.FormatRemaining),
}

func main() {
	window := geometry.Flag("window.geometry", image.Rect(0, 0, 117, 20), "window geometry in pixels")
	battRect := geometry.Flag("battery.geometry", image.Rect(0, 0, 21, 18).Add(image.Pt(1, 2)), "battery icon geometry in pixels")
	borderThickness := flag.Int("border", 1, "battery border thickness in pixels")
	textRect := geometry.Flag("text.geometry", image.Rect(0, 0, 95, 20).Add(image.Pt(22, 0)), "text box geometry in pixels")
	textFont := flag.String("text.font", "DejaVuSans-Bold", "application text font")
	textFontSize := flag.Float64("text.fontsize", 14, "application text font size")
	textInterval := flag.Duration("text.interval", 7*time.Second+500*time.Millisecond, "interval to display each formatted text metric")
	flag.Parse()

	// remaining arguments are text formatters to rotate between
	var formatters []battery.MetricFormatter
	for _, tsrc := range flag.Args() {
		t, err := battery.FormatMetricTemplate(tsrc)
		if err != nil {
			log.Fatalf("template: %v %q", err, tsrc)
		}
		formatters = append(formatters, t)
	}
	if len(formatters) == 0 {
		formatters = append(formatters, defaultFormatters...)
	}

	// Open the specified font.
	ttfpath, err := LocateFont(*textFont)
	if err != nil {
		log.Fatalf("font: %v", err)
	}
	font, err := ReadFontFile(ttfpath)
	if err != nil {
		log.Fatalf("font: %v", err)
	}

	// configure the application window layout
	layout := &AppLayout{
		rect:      *window,
		battRect:  *battRect,
		textRect:  *textRect,
		thickness: *borderThickness,
		DPI:       72,
		font:      font,
		fontSize:  *textFontSize,
	}

	app := NewApp(layout)
	app.BatteryColor = defaultGrey

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

	// begin profiling the battery.  prime the profile by immediately calling
	// the Metrics method.
	metricsc := make(chan *battery.Metrics, 1)
	guage, err := creeperguage.NewCreeperBatteryGuage()
	if err != nil {
		log.Fatal(err)
	}
	batt := battery.NewProfiler(guage)
	go batt.Start(time.Minute, metricsc)
	defer batt.Stop()

	// rotate through all provided formatters (or the default set), sending
	// them to the draw loop at the specified interval.
	formatterc := make(chan battery.MetricFormatter, 1)
	go battery.RotateMetricsFormat(*textInterval, formatterc, formatters...)

	// begin the main draw loop. the draw loop receives updates in the form of
	// new battery metrics and formatters.  The event loop will exit if the
	// draw loop ever terminates.
	go RunApp(dockapp, app, metricsc, formatterc)

	// finally map the window and start the main event loop
	dockapp.Main()
}

// RunApp runs the main loop for the application.
func RunApp(dockapp *dockapp.DockApp, app *App, metrics <-chan *battery.Metrics, formatter <-chan battery.MetricFormatter) {
	defer dockapp.Quit()
	var m *battery.Metrics
	var f battery.MetricFormatter
	for {
		select {
		case m = <-metrics:
		case f = <-formatter:
		}
		if m == nil {
			log.Printf("nil metrics")
			continue
		}
		if f == nil {
			log.Printf("nil formatter")
			continue
		}
		// draw the widget to the screen.
		err := app.Draw(dockapp.Canvas(), m, f)
		if err != nil {
			log.Panic(err)
		}
		dockapp.FlushImage()
	}
}

// AppLayout is configuration the defines the relative geometries of
type AppLayout struct {
	rect      image.Rectangle
	battRect  image.Rectangle
	textRect  image.Rectangle
	thickness int
	font      *truetype.Font
	fontSize  float64
	DPI       float64
}

// App is the battery dockapp.
type App struct {
	Layout       *AppLayout
	BatteryColor color.Color
	EnergyColor  func(*battery.Metrics) color.Color
	maskBattery  image.Image
	maskEnergy   image.Image
	minEnergy    int
	maxEnergy    int
	tt           *freetype.Context
	font         *font.Drawer
}

// NewApp returns a new dockapp.
func NewApp(layout *AppLayout) *App {
	app := &App{
		Layout:       layout,
		BatteryColor: color.Black,
	}
	app.initLayout()
	return app
}

var white = image.NewUniform(color.White)
var black = image.NewUniform(color.Black)
var transparent = image.NewUniform(color.Transparent)
var opaque = image.NewUniform(color.Opaque)

// initLayout constructs two masks for drawing the battery and the remaining
// energy as well as sets the pixel bounds for drawing energy capacity.  the
// masks allow for simplified space-fills and reduced chance of pixel gaps.
func (app *App) initLayout() {
	var zeropt image.Point

	rectOutTop := image.Rectangle{Min: app.Layout.battRect.Min, Max: app.Layout.battRect.Min.Add(image.Point{2, 2})}
	rectOutBottom := rectOutTop.Add(image.Point{Y: app.Layout.battRect.Size().Y - rectOutTop.Size().Y})
	capRect := image.Rectangle{
		Min: image.Point{X: rectOutTop.Min.X, Y: rectOutTop.Max.Y},
		Max: image.Point{X: rectOutBottom.Max.X, Y: rectOutBottom.Min.Y},
	}
	bodyRect := app.Layout.battRect
	bodyRect.Min.X = capRect.Max.X

	// energy will be drawn under the battery shell.  The only place where it
	// is not safe to draw energy is outside the battery on the positive end.
	energyMask := image.NewAlpha(app.Layout.battRect)
	draw.Draw(energyMask, app.Layout.battRect, opaque, zeropt, draw.Over)
	draw.Draw(energyMask, rectOutTop, transparent, zeropt, draw.Src)
	draw.Draw(energyMask, rectOutBottom, transparent, zeropt, draw.Src)
	app.maskEnergy = energyMask

	// the body uses the same mask as the energy with additional transparency
	// inside the battery's shell.  the mask construction is complex because
	// area inside the cap may be exposed.
	bodyMask := image.NewAlpha(app.Layout.battRect)
	draw.Draw(bodyMask, app.Layout.battRect, energyMask, app.Layout.battRect.Min, draw.Over)
	bodyMaskRect := shrinkRect(bodyRect, app.Layout.thickness)
	draw.Draw(bodyMask, bodyMaskRect, transparent, zeropt, draw.Src)
	capMaskRect := shrinkRect(capRect, app.Layout.thickness)
	capMaskRect.Max.X += 2 * app.Layout.thickness
	draw.Draw(bodyMask, capMaskRect, transparent, zeropt, draw.Src)
	app.maskBattery = bodyMask

	// create a freetype.Context to render text.  each time the context is used
	// it must have its SetDst method called.
	app.tt = freetype.NewContext()
	app.tt.SetSrc(black)
	app.tt.SetClip(app.Layout.textRect)
	app.tt.SetDPI(app.Layout.DPI)
	app.tt.SetFont(app.Layout.font)
	app.tt.SetFontSize(app.Layout.fontSize)
	ttopt := &truetype.Options{
		Size: app.Layout.fontSize,
		DPI:  app.Layout.DPI,
	}
	ttface := truetype.NewFace(app.Layout.font, ttopt)
	app.font = &font.Drawer{
		Src:  black,
		Face: ttface,
	}

	// the rectangle in which energy is drawn needs to account for thickness to
	// make the visible percentage more accurate.  after adjustment reduce the
	// energy rect to account for the account of energy drained.  the energy
	// mask makes computing Y bounds largely irrelevant.
	app.minEnergy = capMaskRect.Min.X
	app.maxEnergy = bodyMaskRect.Max.X
}

// Draw renders metrics in the application window with the given formatter.
func (app *App) Draw(img draw.Image, metrics *battery.Metrics, f battery.MetricFormatter) error {
	draw.Draw(img, app.Layout.rect, white, image.Point{}, draw.Over)
	app.drawBattery(img, metrics)
	return app.drawText(img, metrics, f)
}

func (app *App) drawBattery(img draw.Image, metrics *battery.Metrics) {
	var zeropt image.Point

	// shrink the rectangle in which energy is drawn to account for thickness
	// and make the visible percentage more accurate.  after adjustment reduce
	// the energy rect to account for the account of energy drained.
	energyRect := app.Layout.battRect
	energyRect.Min.X = app.minEnergy
	energyRect.Max.X = app.maxEnergy
	energySize := energyRect.Size()
	drain := 1 - metrics.Fraction
	drainSize := int(drain * float64(energySize.X))
	energyRect.Min.X += drainSize

	colorfn := app.EnergyColor
	if colorfn == nil {
		colorfn = DefaultEnergyColor
	}
	energyColor := colorfn(metrics)

	// draw the energy first and overlay the battery shell/border.
	draw.DrawMask(img, energyRect, image.NewUniform(energyColor), zeropt, app.maskEnergy, energyRect.Min, draw.Over)
	draw.DrawMask(img, app.Layout.battRect, image.NewUniform(app.BatteryColor), zeropt, app.maskBattery, app.Layout.battRect.Min, draw.Over)
}

func (app *App) drawText(img draw.Image, metrics *battery.Metrics, f battery.MetricFormatter) error {
	// measure the text so that it can be centered within the text area.  if f
	// is a MaxMetricFormatter use it's MaxFormattedWidth method to determine
	// the appropriate centering position so that a change in metric values
	// (but not formatter) will have a smooth transition in the ui.
	app.font.Dst = img
	text := f.Format(metrics)
	measuretext := text
	if fmax, ok := f.(battery.MaxMetricFormatter); ok {
		measuretext = fmax.MaxFormattedWidth()
	}
	xoffset := app.font.MeasureString(measuretext)
	ttwidth := int(xoffset >> 6)
	ttheight := int(app.tt.PointToFixed(app.Layout.fontSize) >> 6)
	padleft := (app.Layout.textRect.Size().X - ttwidth) / 2
	padtop := (app.Layout.textRect.Size().Y - ttheight) / 2
	x := app.Layout.textRect.Min.X + padleft
	y := app.Layout.textRect.Max.Y - padtop
	app.font.Dot = fixed.P(x, y)
	app.font.DrawString(text)
	return nil
}

func shrinkRect(r image.Rectangle, delta int) image.Rectangle {
	r.Min.X += delta
	r.Min.Y += delta
	r.Max.X -= delta
	r.Max.Y -= delta
	return r
}

var defaultGrey = color.RGBA{R: 0xaa, G: 0xaa, B: 0xaa, A: 0xff}
var defaultRed = color.RGBA{R: 0xff, G: 0x80, B: 0x80, A: 0xff}
var defaultGreen = color.RGBA{R: 0x80, G: 0xff, B: 0x80, A: 0xff}
var defaultYellow = color.RGBA{R: 0xef, G: 0xef, B: 0x40, A: 0xff}

// DefaultEnergyColor returns the default rendering color for battery "energy"
// with the given metrics.
func DefaultEnergyColor(metrics *battery.Metrics) color.Color {
	ecolor := defaultGreen
	if metrics.State == battery.Charging || metrics.State == battery.PendingCharge {
		ecolor = defaultYellow
	} else if metrics.Fraction <= 0.15 {
		ecolor = defaultRed
	}
	return ecolor
}

type imageRecorder struct {
	c     color.Model
	rdraw *image.Rectangle
}

func (r *imageRecorder) ColorModel() color.Model {
	return r.c
}

func (r *imageRecorder) Bounds() image.Rectangle {
	return image.Rectangle{
		Min: image.Pt(int(math.MinInt32), int(math.MinInt32)),
		Max: image.Pt(int(math.MaxInt32), int(math.MaxInt32)),
	}
}

func (r *imageRecorder) At(x, y int) color.Color {
	return r.c.Convert(color.White)
}

func (r *imageRecorder) Set(x, y int, c color.Color) {
	if r.rdraw == nil {
		r.rdraw = &image.Rectangle{
			Min: image.Pt(x, y),
			Max: image.Pt(x, y),
		}
	} else {
		if x < r.rdraw.Min.X {
			r.rdraw.Min.X = x
		}
		if x > r.rdraw.Max.X {
			r.rdraw.Max.X = x
		}
		if y < r.rdraw.Min.Y {
			r.rdraw.Min.Y = y
		}
		if y > r.rdraw.Max.Y {
			r.rdraw.Max.Y = y
		}
	}
}
