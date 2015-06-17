package geometry

import (
	"flag"
	"fmt"
	"image"
	"regexp"
	"strconv"
)

func Contract(r image.Rectangle, pt image.Point) image.Rectangle {
	r.Min = r.Min.Add(pt)
	r.Max = r.Max.Sub(pt)
	return r
}

// this isn't very fun.  but i don't really feel like writing a parser.
var geomRegexp = regexp.MustCompile(`^(?:(\d+)x(\d+))?(?:([+-]\d+)([+-]\d+))?$`)

func Parse(geom string) (rect image.Rectangle, err error) {
	if geom == "" {
		return rect, fmt.Errorf("empty string")
	}
	subs := geomRegexp.FindAllStringSubmatch(geom, -1)
	if len(subs) < 1 {
		return rect, fmt.Errorf("invalid geometry")
	}

	// parse subvalues as integers and assign to size and offet points which
	// describe the rectangle.
	var size image.Point
	var off image.Point
	if subs[0][1] != "" {
		size.X, _ = strconv.Atoi(subs[0][1])
		size.Y, _ = strconv.Atoi(subs[0][2])
	}
	if subs[0][3] != "" {
		off.X, _ = strconv.Atoi(subs[0][3])
		off.Y, _ = strconv.Atoi(subs[0][4])
	}

	rect.Max = size
	rect = rect.Add(off)

	return rect, nil
}

func Format(rect image.Rectangle) string {
	if rect.Min.Eq(image.Point{}) {
		return fmt.Sprintf("%dx%d", rect.Max.X, rect.Max.Y)
	}
	return fmt.Sprintf("%dx%d%+d%+d", rect.Dx(), rect.Dy(), rect.Min.X, rect.Min.Y)
}

func Flag(name string, def image.Rectangle, usage string) *image.Rectangle {
	v := &flagValue{
		rect: &image.Rectangle{},
	}
	*v.rect = def
	flag.Var(v, name, usage)
	return v.rect
}

func FlagVar(r *image.Rectangle, name string, def image.Rectangle, usage string) {
	v := &flagValue{
		rect: &def,
	}
	flag.Var(v, name, usage)
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
