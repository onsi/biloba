package engine

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestWriteScreenshotPNGSerializesReplacementAndPublishesCompleteFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "baseline.png")
	first := solidPNGForAtomicTest(8, 8, color.NRGBA{R: 255, A: 255})
	second := solidPNGForAtomicTest(8, 8, color.NRGBA{B: 255, A: 255})

	originalReplace := replaceScreenshotFile
	var active atomic.Int64
	var maximum atomic.Int64
	replaceScreenshotFile = func(oldPath, newPath string) error {
		current := active.Add(1)
		for {
			observed := maximum.Load()
			if current <= observed || maximum.CompareAndSwap(observed, current) {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
		defer active.Add(-1)
		return originalReplace(oldPath, newPath)
	}
	defer func() { replaceScreenshotFile = originalReplace }()

	var writers sync.WaitGroup
	for _, data := range [][]byte{first, second, first, second} {
		data := data
		writers.Add(1)
		go func() {
			defer writers.Done()
			if err := WriteScreenshotPNG(path, data, 0); err != nil {
				t.Errorf("write screenshot: %v", err)
			}
		}()
	}
	writers.Wait()

	if got := maximum.Load(); got != 1 {
		t.Fatalf("replacement ran %d times concurrently for one path", got)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(contents, first) && !bytes.Equal(contents, second) {
		t.Fatal("published screenshot was neither complete input")
	}
}

func solidPNGForAtomicTest(width, height int, fill color.NRGBA) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.SetNRGBA(x, y, fill)
		}
	}
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		panic(err)
	}
	return out.Bytes()
}
