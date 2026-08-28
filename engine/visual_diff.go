package engine

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
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
	Shifted                                       bool
	Shift                                         image.Point
	Scattered                                     bool
	RasterizationLikely                           bool
	Unchanged                                     string
}

type ScreenshotPaths struct{ Baseline, Actual, Diff string }

func (d ScreenshotDiff) Summary(name string) string {
	if d.DimensionMismatch {
		return fmt.Sprintf(
			"screenshot %q updated — resized from %dx%d to %dx%d",
			name,
			d.BaselineBounds.Dx(),
			d.BaselineBounds.Dy(),
			d.ActualBounds.Dx(),
			d.ActualBounds.Dy(),
		)
	}
	if d.Match {
		return fmt.Sprintf("screenshot %q unchanged", name)
	}
	return fmt.Sprintf(
		"screenshot %q updated — %d of %d pixels changed (%.2f%%), %s",
		name,
		d.DifferingPixels,
		d.TotalPixels,
		d.Fraction*100,
		d.changeShape(),
	)
}

func (d ScreenshotDiff) changeShape() string {
	switch {
	case d.Shifted:
		return fmt.Sprintf("content shifted by (%d,%d)", d.Shift.X, d.Shift.Y)
	case d.Scattered:
		return fmt.Sprintf("%d scattered regions", d.RegionCount)
	case d.RegionCount == 1:
		return "one box"
	default:
		return fmt.Sprintf("%d boxes", d.RegionCount)
	}
}

const (
	maxReportedScreenshotRegions = 5
	diagnosticGridCell           = 8
	maxShiftOffset               = 4
	shiftSearchWindow            = 400
	shiftMinFraction             = 0.02
	shiftResidualRatio           = 0.2
	scatteredMinRegions          = 12
	scatteredMaxRegionArea       = 0.01
	scatteredMinSpan             = 0.5
	rasterizationChannelDelta    = 8
	unchangedSideFraction        = 0.5
)

func CompareScreenshotPNGs(baseline, actual []byte, tolerance ScreenshotTolerance) (ScreenshotDiff, []byte, error) {
	if err := validateScreenshotTolerance(tolerance); err != nil {
		return ScreenshotDiff{}, nil, err
	}
	if err := validateScreenshotPNG(baseline, 0); err != nil {
		return ScreenshotDiff{}, nil, fmt.Errorf("validate baseline PNG: %w", err)
	}
	if err := validateScreenshotPNG(actual, 0); err != nil {
		return ScreenshotDiff{}, nil, fmt.Errorf("validate actual PNG: %w", err)
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
	allRegions, combined := changedRegions(changed, w, h)
	diff.RegionCount = len(allRegions)
	diff.Scattered = scatteredRegions(allRegions, combined, w, h)
	diff.Regions = allRegions
	if len(diff.Regions) > maxReportedScreenshotRegions {
		diff.Regions = diff.Regions[:maxReportedScreenshotRegions]
	}
	diff.Shift, diff.Shifted = detectScreenshotShift(base, act, diff.Fraction, diff.DifferingPixels, tolerance.ChannelDelta)
	diff.RasterizationLikely = diff.DifferingPixels > 0 && diff.MaxChannelDelta <= rasterizationChannelDelta
	diff.Unchanged = unchangedSide(diff, w, h)
	diffImage := image.NewNRGBA(act.Bounds())
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			pixel := act.NRGBAAt(x, y)
			if changed[y*w+x] {
				pixel = color.NRGBA{R: 255, B: 255, A: 255}
			} else {
				luma := (299*int(pixel.R) + 587*int(pixel.G) + 114*int(pixel.B)) / 1000
				washed := uint8(255 - (255-luma)/4)
				pixel = color.NRGBA{R: washed, G: washed, B: washed, A: 255}
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

func validateScreenshotTolerance(tolerance ScreenshotTolerance) error {
	if tolerance.ChannelDelta < 0 || tolerance.ChannelDelta > 255 {
		return fmt.Errorf("channel tolerance must be between 0 and 255")
	}
	if math.IsNaN(tolerance.PixelFraction) || math.IsInf(tolerance.PixelFraction, 0) || tolerance.PixelFraction < 0 || tolerance.PixelFraction > 1 {
		return fmt.Errorf("pixel tolerance must be between 0 and 1")
	}
	return nil
}

func (d ScreenshotDiff) Diagnose(name string, paths ScreenshotPaths) string {
	var out strings.Builder
	fmt.Fprintf(&out, "screenshot %q differs from baseline\n", name)
	if d.DimensionMismatch {
		fmt.Fprintf(&out, "  baseline is %dx%d, actual is %dx%d%s\n", d.BaselineBounds.Dx(), d.BaselineBounds.Dy(), d.ActualBounds.Dx(), d.ActualBounds.Dy(), dimensionDeltaWords(d))
	} else {
		fmt.Fprintf(&out, "  %d of %d pixels differ (%.2f%%), max channel delta %d\n", d.DifferingPixels, d.TotalPixels, d.Fraction*100, d.MaxChannelDelta)
		if d.RasterizationLikely {
			fmt.Fprintf(&out, "  every differing pixel differs by <= %d — a rasterisation or compositing difference\n", d.MaxChannelDelta)
		}
		if d.Shifted {
			fmt.Fprintf(&out, "  changed region: uniform shift of the whole image (dx=%d, dy=%d)\n", d.Shift.X, d.Shift.Y)
		} else if d.Scattered {
			fmt.Fprintf(&out, "  changed region: scattered — %d regions spread across the image\n", d.RegionCount)
		} else if len(d.Regions) == 1 {
			r := d.Regions[0].Rect
			fmt.Fprintf(&out, "  changed region: one box, (%d,%d)-(%d,%d)\n", r.Min.X, r.Min.Y, r.Max.X, r.Max.Y)
		} else {
			fmt.Fprintf(&out, "  changed regions: %d boxes\n", d.RegionCount)
		}
		if d.Unchanged != "" {
			fmt.Fprintf(&out, "  unchanged: %s\n", d.Unchanged)
		}
	}
	for _, entry := range [][2]string{{"baseline:", paths.Baseline}, {"actual:", paths.Actual}, {"diff:", paths.Diff}} {
		if entry[1] != "" {
			fmt.Fprintf(&out, "  %-9s %s\n", entry[0], entry[1])
		}
	}
	return out.String()
}

func unchangedSide(diff ScreenshotDiff, width, height int) string {
	if diff.Shifted || diff.Scattered || len(diff.Regions) == 0 || diff.RegionCount != len(diff.Regions) || width == 0 || height == 0 {
		return ""
	}
	box := diff.Regions[0].Rect
	for _, region := range diff.Regions[1:] {
		box = box.Union(region.Rect)
	}
	type side struct {
		fraction float64
		text     string
	}
	sides := []side{{float64(height-box.Max.Y) / float64(height), fmt.Sprintf("everything below y=%d", box.Max.Y)}, {float64(box.Min.Y) / float64(height), fmt.Sprintf("everything above y=%d", box.Min.Y)}, {float64(width-box.Max.X) / float64(width), fmt.Sprintf("everything right of x=%d", box.Max.X)}, {float64(box.Min.X) / float64(width), fmt.Sprintf("everything left of x=%d", box.Min.X)}}
	best := sides[0]
	for _, candidate := range sides[1:] {
		if candidate.fraction > best.fraction {
			best = candidate
		}
	}
	if best.fraction < unchangedSideFraction {
		return ""
	}
	return best.text
}

func dimensionDeltaWords(d ScreenshotDiff) string {
	dw, dh := d.ActualBounds.Dx()-d.BaselineBounds.Dx(), d.ActualBounds.Dy()-d.BaselineBounds.Dy()
	parts := []string{}
	if dh != 0 {
		direction := "taller"
		if dh < 0 {
			direction = "shorter"
		}
		parts = append(parts, fmt.Sprintf("%dpx %s", absInt(dh), direction))
	}
	if dw != 0 {
		direction := "wider"
		if dw < 0 {
			direction = "narrower"
		}
		parts = append(parts, fmt.Sprintf("%dpx %s", absInt(dw), direction))
	}
	if len(parts) == 0 {
		return ""
	}
	return " (" + strings.Join(parts, " and ") + ")"
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

func changedRegions(changed []bool, w, h int) ([]ScreenshotRegion, image.Rectangle) {
	columns, rows := (w+diagnosticGridCell-1)/diagnosticGridCell, (h+diagnosticGridCell-1)/diagnosticGridCell
	marked := make([]bool, columns*rows)
	for index, isChanged := range changed {
		if isChanged {
			x, y := index%w, index/w
			marked[(y/diagnosticGridCell)*columns+x/diagnosticGridCell] = true
		}
	}
	component := make([]int, len(marked))
	for i := range component {
		component[i] = -1
	}
	componentCount := 0
	for start, isMarked := range marked {
		if !isMarked || component[start] >= 0 {
			continue
		}
		component[start] = componentCount
		queue := []int{start}
		for len(queue) > 0 {
			cell := queue[0]
			queue = queue[1:]
			cx, cy := cell%columns, cell/columns
			for _, neighbor := range [][2]int{{cx - 1, cy - 1}, {cx, cy - 1}, {cx + 1, cy - 1}, {cx - 1, cy}, {cx + 1, cy}, {cx - 1, cy + 1}, {cx, cy + 1}, {cx + 1, cy + 1}} {
				nx, ny := neighbor[0], neighbor[1]
				if nx >= 0 && nx < columns && ny >= 0 && ny < rows {
					next := ny*columns + nx
					if marked[next] && component[next] < 0 {
						component[next] = componentCount
						queue = append(queue, next)
					}
				}
			}
		}
		componentCount++
	}
	regions := make([]ScreenshotRegion, componentCount)
	combined := image.Rectangle{}
	for index, isChanged := range changed {
		if !isChanged {
			continue
		}
		x, y := index%w, index/w
		id := component[(y/diagnosticGridCell)*columns+x/diagnosticGridCell]
		pixel := image.Rect(x, y, x+1, y+1)
		if regions[id].Count == 0 {
			regions[id].Rect = pixel
		} else {
			regions[id].Rect = regions[id].Rect.Union(pixel)
		}
		regions[id].Count++
	}
	for _, region := range regions {
		if combined.Empty() {
			combined = region.Rect
		} else {
			combined = combined.Union(region.Rect)
		}
	}
	sort.Slice(regions, func(i, j int) bool { return regions[i].Count > regions[j].Count })
	return regions, combined
}

func scatteredRegions(regions []ScreenshotRegion, combined image.Rectangle, w, h int) bool {
	if len(regions) < scatteredMinRegions || w == 0 || h == 0 {
		return false
	}
	limit := float64(w*h) * scatteredMaxRegionArea
	for _, region := range regions {
		if float64(region.Rect.Dx()*region.Rect.Dy()) > limit {
			return false
		}
	}
	return float64(combined.Dx()) > scatteredMinSpan*float64(w) && float64(combined.Dy()) > scatteredMinSpan*float64(h)
}

func detectScreenshotShift(base, actual *image.NRGBA, fraction float64, differing, channelTolerance int) (image.Point, bool) {
	w, h := base.Bounds().Dx(), base.Bounds().Dy()
	if fraction < shiftMinFraction || differing == 0 || w <= 4*maxShiftOffset || h <= 4*maxShiftOffset {
		return image.Point{}, false
	}
	bestCount, best := -1, image.Point{}
	cropWidth, cropHeight := min(w, shiftSearchWindow), min(h, shiftSearchWindow)
	x0, y0 := (w-cropWidth)/2, (h-cropHeight)/2
	for dy := -maxShiftOffset; dy <= maxShiftOffset; dy++ {
		for dx := -maxShiftOffset; dx <= maxShiftOffset; dx++ {
			if dx == 0 && dy == 0 {
				continue
			}
			count := shiftedWindowDifferenceCount(base, actual, x0, y0, cropWidth, cropHeight, dx, dy, channelTolerance)
			if bestCount < 0 || count < bestCount {
				bestCount, best = count, image.Pt(dx, dy)
			}
		}
	}
	if bestCount < 0 {
		return image.Point{}, false
	}
	fullResidual := shiftedDifferenceCount(base, actual, best.X, best.Y, channelTolerance)
	if float64(fullResidual) >= shiftResidualRatio*float64(differing) {
		return image.Point{}, false
	}
	return best, true
}

func shiftedWindowDifferenceCount(base, actual *image.NRGBA, x0, y0, width, height, dx, dy, tolerance int) int {
	imageWidth, imageHeight := base.Bounds().Dx(), base.Bounds().Dy()
	count := 0
	for y := y0; y < y0+height; y++ {
		ty := y + dy
		if ty < 0 || ty >= imageHeight {
			count += width
			continue
		}
		for x := x0; x < x0+width; x++ {
			tx := x + dx
			if tx < 0 || tx >= imageWidth || maxChannelDelta(base.NRGBAAt(x, y), actual.NRGBAAt(tx, ty)) > tolerance {
				count++
			}
		}
	}
	return count
}

func shiftedDifferenceCount(base, actual *image.NRGBA, dx, dy, tolerance int) int {
	w, h := base.Bounds().Dx(), base.Bounds().Dy()
	count := 0
	for y := 0; y < h; y++ {
		ty := y + dy
		if ty < 0 || ty >= h {
			count += w
			continue
		}
		for x := 0; x < w; x++ {
			tx := x + dx
			if tx < 0 || tx >= w {
				count++
				continue
			}
			if maxChannelDelta(base.NRGBAAt(x, y), actual.NRGBAAt(tx, ty)) > tolerance {
				count++
			}
		}
	}
	return count
}
