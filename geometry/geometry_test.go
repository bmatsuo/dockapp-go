package geometry

import (
	"image"
	"strings"
	"testing"
)

func TestParseGeometry(t *testing.T) {
	for i, test := range []struct {
		s string
		r image.Rectangle
	}{
		{"1x2", image.Rect(0, 0, 1, 2)},
		{"1x2+3+4", image.Rect(3, 4, 4, 6)},
		{"1x2-3-4", image.Rect(-3, -4, -2, -2)},
	} {
		r, err := parseGeometry(test.s)
		if err != nil {
			t.Errorf("test %d: %v", i, err)
		}
		if r != test.r {
			t.Errorf("test %d: %v", i, r)
		}
	}
}

func TestParseGeometryError(t *testing.T) {
	for i, test := range []struct {
		s       string
		errtext string
	}{
		{"abc", "width"},
		{"1e3", "'x'"},
		{"0xDEADBEEF", "height"},
		{"1x1x1", "x offset"},
		{"1x1+1", "y offset"},
		{"1x1+1+1+1", "end of input"},
	} {
		r, err := parseGeometry(test.s)
		if err == nil {
			t.Errorf("test %d: expected error %q", i, test.errtext)
		} else if !strings.Contains(err.Error(), test.errtext) {
			t.Errorf("test %d: expected %q %v", i, test.errtext, err)
		}
		if r != image.ZR {
			t.Errorf("test %d: %v", i, r)
		}
	}
}

func BenchmarkParseGeometry_regexp(b *testing.B) {
	expect := image.Rect(1920, 0, 1920+1920, 1080)
	for i := 0; i < b.N; i++ {
		geom, err := Parse("1920x1080+1920+0")
		if err != nil {
			b.Fatal(err)
		}
		if geom != expect {
			b.Fatalf("%v (expect %v)", geom, expect)
		}
	}
}

func BenchmarkParseGeometry(b *testing.B) {
	expect := image.Rect(1920, 0, 1920+1920, 1080)
	for i := 0; i < b.N; i++ {
		geom, err := parseGeometry("1920x1080+1920+0")
		if err != nil {
			b.Fatal(err)
		}
		if geom != expect {
			b.Fatalf("%v (expect %v)", geom, expect)
		}
	}
}
