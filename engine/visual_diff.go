package engine

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"sort"
	"strings"
)

type ScreenshotTolerance struct {
	ChannelDelta  int
	PixelFraction float64
}

type ScreenshotRegion struct {
	Rect  image.Rectangle
	Count int
}

type ScreenshotDiff struct {
	Match, DimensionMismatch                      bool
	BaselineBounds, ActualBounds                  image.Rectangle
	TotalPixels, DifferingPixels, MaxChannelDelta int
	Fraction                                      float64
	Regions                                       []ScreenshotRegion
	RegionCount                                   int
}

type ScreenshotPaths struct{ Baseline, Actual, Diff string }

func CompareScreenshotPNGs(baseline, actual []byte, tolerance ScreenshotTolerance) (ScreenshotDiff, []byte, error) {
	if tolerance.ChannelDelta < 0 || tolerance.ChannelDelta > 255 {
		return ScreenshotDiff{}, nil, fmt.Errorf("channel tolerance must be between 0 and 255")
	}
	if tolerance.PixelFraction < 0 || tolerance.PixelFraction > 1 {
		return ScreenshotDiff{}, nil, fmt.Errorf("pixel tolerance must be between 0 and 1")
	}
	base, err := decodeScreenshot(baseline)
	if err != nil {
		return ScreenshotDiff{}, nil, fmt.Errorf("decode baseline PNG: %w", err)
	}
	act, err := decodeScreenshot(actual)
	if err != nil {
		return ScreenshotDiff{}, nil, fmt.Errorf("decode actual PNG: %w", err)
	}
	diff := ScreenshotDiff{BaselineBounds: base.Bounds(), ActualBounds: act.Bounds()}
	if base.Bounds() != act.Bounds() {
		diff.DimensionMismatch = true
		return diff, nil, nil
	}
	w, h := base.Bounds().Dx(), base.Bounds().Dy()
	diff.TotalPixels = w * h
	changed := make([]bool, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := y*w + x
			delta := maxChannelDelta(base.NRGBAAt(x, y), act.NRGBAAt(x, y))
			if delta > diff.MaxChannelDelta {
				diff.MaxChannelDelta = delta
			}
			if delta > tolerance.ChannelDelta {
				changed[i] = true
				diff.DifferingPixels++
			}
		}
	}
	if diff.TotalPixels > 0 {
		diff.Fraction = float64(diff.DifferingPixels) / float64(diff.TotalPixels)
	}
	diff.Match = diff.DifferingPixels == 0 || diff.Fraction <= tolerance.PixelFraction
	if diff.Match {
		return diff, nil, nil
	}
	diff.Regions = changedRegions(changed, w, h)
	diff.RegionCount = len(diff.Regions)
	diffImage := image.NewNRGBA(act.Bounds())
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			pixel := act.NRGBAAt(x, y)
			if changed[y*w+x] {
				pixel = color.NRGBA{R: 255, B: 160, A: 255}
			} else {
				grey := uint8((uint16(pixel.R) + uint16(pixel.G) + uint16(pixel.B)) / 6)
				pixel = color.NRGBA{R: grey, G: grey, B: grey, A: 255}
			}
			diffImage.SetNRGBA(x, y, pixel)
		}
	}
	var out bytes.Buffer
	if err := png.Encode(&out, diffImage); err != nil {
		return ScreenshotDiff{}, nil, err
	}
	return diff, out.Bytes(), nil
}

func (d ScreenshotDiff) Diagnose(name string, paths ScreenshotPaths) string {
	var out strings.Builder
	fmt.Fprintf(&out, "screenshot %q differs from baseline\n", name)
	if d.DimensionMismatch {
		fmt.Fprintf(&out, "  baseline is %dx%d, actual is %dx%d\n", d.BaselineBounds.Dx(), d.BaselineBounds.Dy(), d.ActualBounds.Dx(), d.ActualBounds.Dy())
	} else {
		fmt.Fprintf(&out, "  %d of %d pixels differ (%.2f%%), max channel delta %d\n", d.DifferingPixels, d.TotalPixels, d.Fraction*100, d.MaxChannelDelta)
		if len(d.Regions) == 1 {
			r := d.Regions[0].Rect
			fmt.Fprintf(&out, "  changed region: one box, (%d,%d)-(%d,%d)\n", r.Min.X, r.Min.Y, r.Max.X, r.Max.Y)
		} else {
			fmt.Fprintf(&out, "  changed regions: %d boxes\n", d.RegionCount)
		}
	}
	for _, entry := range [][2]string{{"baseline:", paths.Baseline}, {"actual:", paths.Actual}, {"diff:", paths.Diff}} {
		if entry[1] != "" {
			fmt.Fprintf(&out, "  %-9s %s\n", entry[0], entry[1])
		}
	}
	return out.String()
}

func decodeScreenshot(data []byte) (*image.NRGBA, error) {
	source, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	bounds := source.Bounds()
	result := image.NewNRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	draw.Draw(result, result.Bounds(), source, bounds.Min, draw.Src)
	return result, nil
}

func maxChannelDelta(a, b color.NRGBA) int {
	values := [4]int{absInt(int(a.R) - int(b.R)), absInt(int(a.G) - int(b.G)), absInt(int(a.B) - int(b.B)), absInt(int(a.A) - int(b.A))}
	max := 0
	for _, v := range values {
		if v > max {
			max = v
		}
	}
	return max
}
func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

func changedRegions(changed []bool, w, h int) []ScreenshotRegion {
	seen := make([]bool, len(changed))
	regions := []ScreenshotRegion{}
	for start, isChanged := range changed {
		if !isChanged || seen[start] {
			continue
		}
		seen[start] = true
		queue := []int{start}
		rect := image.Rect(start%w, start/w, start%w+1, start/w+1)
		count := 0
		for len(queue) > 0 {
			i := queue[0]
			queue = queue[1:]
			count++
			x, y := i%w, i/w
			rect = rect.Union(image.Rect(x, y, x+1, y+1))
			for _, n := range [][2]int{{x - 1, y}, {x + 1, y}, {x, y - 1}, {x, y + 1}} {
				nx, ny := n[0], n[1]
				if nx >= 0 && nx < w && ny >= 0 && ny < h {
					j := ny*w + nx
					if changed[j] && !seen[j] {
						seen[j] = true
						queue = append(queue, j)
					}
				}
			}
		}
		regions = append(regions, ScreenshotRegion{Rect: rect, Count: count})
	}
	sort.Slice(regions, func(i, j int) bool { return regions[i].Count > regions[j].Count })
	return regions
}
