package main

import (
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/golang/freetype"
	"github.com/golang/freetype/truetype"
)

func ReadFontFile(path string) (*truetype.Font, error) {
	if ext := filepath.Ext(path); ext != ".ttf" {
		return nil, fmt.Errorf("cannot %s file as a font", ext)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return ReadFont(f)
}

func ReadFont(r io.Reader) (*truetype.Font, error) {
	ttfraw, err := ioutil.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("read: %v", err)
	}
	font, err := freetype.ParseFont(ttfraw)
	if err != nil {
		return nil, err
	}
	return font, err
}

// systemFontGlobs is a set of location glob prefixes used to search for fonts
// on the local system.
var systemFontGlobs = []string{
	"/usr/share/fonts/truetype/*", // Ubuntu 14.04, Debian???
}

// LocateFont does its best to locate truetype fonts on the local system.
// LocateFont can accept absolute paths, full basenames, or (relative) glob
// patterns.  Glob patterns passed to LocateFont are assumed to end in "*.ttf"
// and the suffix may be omitted from the name argument.
//		LocateFont("/usr/share/fonts/truetype/freefont/FreeMonoBold.ttf")
//		LocateFont("Ubuntu-B.ttf")
//		LocateFont("DejaVuSans-Bold")
func LocateFont(name string) (string, error) {
	if filepath.IsAbs(name) {
		_, err := os.Stat(name)
		if err != nil {
			return "", err
		}
		return name, nil
	}
	for _, base := range systemFontGlobs {
		namepat := name
		if !strings.HasSuffix(name, ".ttf") {
			namepat += "*.ttf"
		}
		pat := filepath.Join(base, namepat)
		files, err := filepath.Glob(pat)
		if err != nil {
			log.Printf("glob: %v", err)
			continue
		}
		if len(files) > 1 {
			log.Printf("ambiguous font name: %q", name)
		}
		if len(files) > 0 {
			return files[0], nil
		}
	}
	return "", fmt.Errorf("no font found")
}
