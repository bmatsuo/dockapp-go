package geometry

import (
	"flag"
	"image"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
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

func TestParse_error(t *testing.T) {
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

func TestFlag(t *testing.T) {
	if flagfn(nil) == nil {
		t.Errorf("nil func returned")
	}

	fs := flag.NewFlagSet("testcmd", flag.ContinueOnError)
	var r1, r2 *image.Rectangle
	r2 = &image.Rectangle{}
	def := image.Rectangle{Min: image.Pt(3, 4), Max: image.Pt(4, 6)} // 1x2+3+4
	r1 = defineFlag(fs, nil, "t1", def, "the first test")
	if r1 == nil {
		t.Errorf("defineFlag returned nil")
	}
	r2 = defineFlag(fs, r2, "t2", def, "the second test")
	if r1 == nil {
		t.Errorf("defineFlag returned nil")
	}
	if r2 != r2 { // pointers should be equal
		t.Errorf("defineFlag returned a different pointer")
	}
	err := fs.Parse([]string{"-t2=1x1+1+1"})
	if err != nil {
		t.Errorf("parse error: %v", err)
	}
	if *r1 != def {
		t.Errorf("r1: %#v", *r1)
	}
	if *r2 == def {
		t.Errorf("r2: %#v", r2)
	}
}

func BenchmarkParse(b *testing.B) {
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
