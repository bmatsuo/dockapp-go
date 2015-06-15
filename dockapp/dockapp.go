package dockapp

import (
	"fmt"
	"image"
	"image/draw"
	"log"

	"github.com/BurntSushi/xgbutil"
	"github.com/BurntSushi/xgbutil/icccm"
	"github.com/BurntSushi/xgbutil/xevent"
	"github.com/BurntSushi/xgbutil/xgraphics"
	"github.com/BurntSushi/xgbutil/xwindow"
)

// DockApp holds references to an xwindow.Window and ximage.Image for the
// process and executes the x11 main event loop.
type DockApp struct {
	x   *xgbutil.XUtil
	img *xgraphics.Image
	win *xwindow.Window
}

// Main maps the dockapp window to the display and runs the main x event loop.
func (app *DockApp) Main() {
	app.win.Map()
	xevent.Main(app.x)
}

// Canvas returns a an image to be drawn to the screen dockapp window.  After
// drawing to the returned image FlushImage must be called in order to reflect
// the changes on the display.
func (app *DockApp) Canvas() draw.Image {
	return app.img
}

// Quit terminates the main event loop.
func (app *DockApp) Quit() {
	xevent.Quit(app.x)
}

// Destroy releases window and image resources associated with the dockapp.
// Destroy does not close the underlying connection with the x server.
func (app *DockApp) Destroy() {
	app.img.Destroy()
	app.win.Destroy()
}

// FlushImage writes dockapp window data and updates the screen with the
// contents of app.Canvas().
func (app *DockApp) FlushImage() {
	app.img.XDraw()
	app.img.XPaint(app.win.Id)
}

// NewDockApp allocates and initializes a new DockApp.  NewDockApp does not
// initialize the window contents and does not map the window to the display
// screen.  The window is mapped to the screen when the Main method is called
// on the returned DockApp.
func New(x *xgbutil.XUtil, rect image.Rectangle) (*DockApp, error) {
	win, err := xwindow.Generate(x)
	if err != nil {
		log.Fatalf("generate window: %v", err)
	}
	win.Create(x.RootWin(), 0, 0, rect.Size().X, rect.Size().Y, 0)

	// Set WM hints so that Openbox puts the window into the dock.
	hints := &icccm.Hints{
		Flags:        icccm.HintState | icccm.HintIconWindow,
		InitialState: icccm.StateWithdrawn,
		IconWindow:   win.Id,
		WindowGroup:  win.Id,
	}
	err = icccm.WmHintsSet(x, win.Id, hints)
	if err != nil {
		win.Destroy()
		return nil, fmt.Errorf("wm hints: %v", err)
	}
	img := xgraphics.New(x, rect)
	err = img.XSurfaceSet(win.Id)
	if err != nil {
		img.Destroy()
		win.Destroy()
		return nil, fmt.Errorf("xsurface set: %v", err)
	}
	app := &DockApp{
		x:   x,
		img: img,
		win: win,
	}
	return app, nil
}
