package geometry

import (
	"flag"
	"fmt"
	"image"
	"strconv"
	"unicode"

	"github.com/bmatsuo/go-lexer"
)

// Contract returns a rectangle resulting from contracting r by x on each side.
func Contract(r image.Rectangle, n int) image.Rectangle {
	return Contract2(r, n, n)
}

// Contract2 returns a rectangle resulting from contracting r by x in each side
// and y on top and bottom.  The rectangle returned by Contract2 has the same
// center of mass as r.
func Contract2(r image.Rectangle, x, y int) image.Rectangle {
	return Contract4(r, x, y, x, y)
}

// Contract4 returns a rectangle resulting from adding image.Pt(xmin, ymin) to
// r.Min and subtracting image.Pt(xmax, ymax) from r.Max.
func Contract4(r image.Rectangle, xmin, ymin, xmax, ymax int) image.Rectangle {
	return image.Rectangle{
		Min: r.Min.Add(image.Pt(xmin, ymin)),
		Max: r.Max.Sub(image.Pt(xmax, ymax)),
	}
}

// Parse returns an image.Rectangle corresponding to the given geometry string.
func Parse(geom string) (rect image.Rectangle, err error) {
	return parseGeometry(geom)
}

// Format renders the given image.Rectangle as a geometry string.
func Format(rect image.Rectangle) string {
	if rect.Min.Eq(image.Point{}) {
		return fmt.Sprintf("%dx%d", rect.Max.X, rect.Max.Y)
	}
	return fmt.Sprintf("%dx%d%+d%+d", rect.Dx(), rect.Dy(), rect.Min.X, rect.Min.Y)
}

var defaultFlagFunc = flag.Var

func flagfn(fs *flag.FlagSet) func(flag.Value, string, string) {
	if fs != nil {
		return fs.Var
	}
	return defaultFlagFunc
}

func defineFlag(fs *flag.FlagSet, r *image.Rectangle, name string, def image.Rectangle, usage string) *image.Rectangle {
	define := flagfn(fs)
	if r == nil {
		r = &def
	} else {
		*r = def
	}
	v := &flagValue{rect: r}
	define(v, name, usage)
	return r
}

// Flag registers name with the flag package.
func Flag(name string, def image.Rectangle, usage string) *image.Rectangle {
	return defineFlag(nil, nil, name, def, usage)
}

// FlagVar is like Flag but takes the pointer to an image.Rectangle for
// assignment.
func FlagVar(r *image.Rectangle, name string, def image.Rectangle, usage string) {
	defineFlag(nil, r, name, def, usage)
}

type flagValue struct {
	rect *image.Rectangle
}

func (v *flagValue) String() string {
	return Format(*v.rect)
}

func (v *flagValue) Set(s string) error {
	rect, err := Parse(s)
	if err != nil {
		return err
	}
	*v.rect = rect
	return nil
}

func parseGeometry(s string) (image.Rectangle, error) {
	lex := lexer.New(lexGeometry, s)

	xdim, err := _parseInt(lex.Next())
	if err != nil {
		return image.ZR, err
	}
	ydim, err := _parseInt(lex.Next())
	if err != nil {
		return image.ZR, err
	}
	xoffset, err := _parseInt(lex.Next())
	if err == errEOF {
		r := image.Rect(0, 0, xdim, ydim)
		return r, nil
	}
	if err != nil {
		return image.ZR, err
	}
	yoffset, err := _parseInt(lex.Next())
	if err != nil {
		return image.ZR, err
	}

	item := lex.Next()
	err = item.Err()
	if err != nil {
		return image.ZR, err
	}
	if item.Type != lexer.ItemEOF {
		return image.ZR, fmt.Errorf("geometry: expected end of input")
	}

	r := image.Rect(xoffset, yoffset, xdim+xoffset, ydim+yoffset)
	return r, nil
}

var errEOF = fmt.Errorf("EOF")

func _parseInt(item *lexer.Item) (int, error) {
	err := item.Err()
	if err != nil {
		return 0, err
	}
	if item.Type == lexer.ItemEOF {
		return 0, errEOF
	}
	x, err := strconv.ParseInt(item.Value, 10, 0)
	return int(x), err
}

const (
	itemDimension lexer.ItemType = iota
	itemOffset
)

func lexGeometry(lex *lexer.Lexer) lexer.StateFn {
	if !_lexDimension(lex) {
		return lex.Errorf("geometry: expected width")
	}
	if !lex.Accept("x") {
		return lex.Errorf("geometry: expected delimiter 'x'")
	}
	lex.Ignore()
	if !_lexDimension(lex) {
		return lex.Errorf("geometry: expected height")
	}

	return lexOffset
}

func lexOffset(lex *lexer.Lexer) lexer.StateFn {
	if !_lexOffset(lex) {
		if lex.Current() != "" {
			return lex.Errorf("geometry: expected x offset")
		}
		if lexer.IsEOF(lex.Peek()) {
			return nil
		}
		return lex.Errorf("geometry: expected x offset")
	}
	if !_lexOffset(lex) {
		return lex.Errorf("geometry: expected y offset")
	}

	if !lexer.IsEOF(lex.Peek()) {
		return lex.Errorf("geometry: expected end of input")
	}

	return nil
}

func _lexDimension(lex *lexer.Lexer) bool {
	if lex.AcceptRunFunc(unicode.IsDigit) == 0 {
		return false
	}
	lex.Emit(itemDimension)
	return true
}

func _lexOffset(lex *lexer.Lexer) bool {
	if !lex.Accept("-+") {
		return false
	}
	if lex.AcceptRunFunc(unicode.IsDigit) == 0 {
		return false
	}
	lex.Emit(itemOffset)
	return true
}
