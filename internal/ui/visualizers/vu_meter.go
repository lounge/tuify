package visualizers

import (
	"math"
	"strings"

	"github.com/lounge/tuify/internal/audio"
)

const (
	// vuSmoothingAlpha gives a ~300 ms exponential time constant at the
	// 33 ms (≈30 FPS) visualizer tick: α = 1 − exp(−Δt/τ) ≈ 0.104.
	vuSmoothingAlpha = 0.105

	vuDbMin = -20.0
	// vuDbMax pins the right-hand arc edge to +3 dB so the "+3" tick
	// sits at the end of the curve, mirroring the "-20" tick at the
	// left edge.
	vuDbMax          = 3.0
	vuDbPivot        = 0.0    // dB value at which dbToAngleRad's two linear segments meet
	vuNoiseFloorPeak = 1.0e-3 // ≈ −60 dB log10 clamp for very quiet channels

	// Total sweep ≈ 40°. A shallow arc reads as the wide, nearly-flat
	// TEAC-style face rather than a tight protractor curve. The radius
	// is sized to fill horizontally, so the visible arc rise is small.
	vuSweepHalfDeg = 20.0

	// vuThetaZeroFrac places the dB pivot (0 dB) at theta =
	// vuThetaZeroFrac * sweep. Pushing it right of the geometric center
	// hands more angular space to the positive-dB segment than its 3 dB
	// of range would otherwise win under a single linear map.
	vuThetaZeroFrac = 0.55

	// Sub-cell margin between the leftmost / rightmost arc points and
	// the pane edges, so the leftmost and rightmost tick labels have
	// room to render without clipping.
	vuLabelEdgeMargin = 4

	// vuArcApexSubRow places the arc's apex (theta = 0) at this sub-row,
	// leaving cell rows 0–1 above it for tick labels and stalks.
	vuArcApexSubRow = 4

	// Needle geometry. tipFrac retracts the tip slightly from r so the
	// needle doesn't touch the arc; baseFrac extends the visible base
	// past the arc-area midpoint for a tall stroke.
	vuNeedleTipFrac  = 0.97
	vuNeedleBaseFrac = 1.6

	// A terminal cell is ~2× taller than wide. To make the arc render as
	// a visually round curve, vertical sub-cell steps cost twice as much
	// distance as horizontal ones: convert sub-cell sy → visual vy by
	// multiplying by vuYAspect.
	vuYAspect = 2.0

	vuMinDialWidth  = 22 // absolute minimum width for any single dial render
	vuMinDialHeight = 8
	vuMaxDialWidth  = 60
	vuMaxDialHeight = 18
	vuDialGap       = 2 // horizontal gap between side-by-side dials
	vuStackGap      = 1 // vertical gap between stacked dials

	// vuSideBySideMinWidth is the per-dial width below which the layout
	// snaps to stacked. Larger than vuMinDialWidth so narrow-but-renderable
	// dials prefer stacking over a cramped side-by-side.
	vuSideBySideMinWidth = 44

	// Cell row layout within a single dial:
	//   rows 0..N-3 — arc + needle, with tick number labels overlaid above
	//                 each tick's arc point so they trace the curve
	//   row N-2     — blank spacer below the dial
	//   row N-1     — channel label ("LEFT" / "RIGHT")
	vuBottomReserve = 2
)

// vuCellKind ranks the marks that can occupy a sub-cell; higher kinds
// win when two marks overlap (e.g. needle over arc). The kind drives
// brightness only — hue is derived from the cell's column so the dial
// uses the same green→purple sweep as the band visualizers.
type vuCellKind uint8

const (
	vuKindEmpty vuCellKind = iota
	vuKindArc
	vuKindTick
	vuKindNeedle
)

// VUMeter renders two analog-style VU meters (LEFT and RIGHT) driven
// by FrequencyData.LeftLevel / RightLevel. Needle ballistics approximate
// the classic IEC ~300 ms integration time. The dB scale is −20…+3
// mapped piecewise so the positive segment occupies more angular space
// than its dB range alone would warrant; cell color follows the same
// green→purple sweep that the band visualizers use.
type VUMeter struct {
	audioData *audio.FrequencyData
	leftDb    float64
	rightDb   float64
	inited    bool
}

func NewVUMeter() *VUMeter {
	return &VUMeter{}
}

func (v *VUMeter) Init(seed string, durationMs int) {
	v.leftDb = vuDbMin
	v.rightDb = vuDbMin
	v.inited = true
}

func (v *VUMeter) SetAudioData(data *audio.FrequencyData) {
	v.audioData = data
}

func (v *VUMeter) Advance() {
	if !v.inited {
		return
	}
	targetL := vuDbMin
	targetR := vuDbMin
	if v.audioData != nil {
		targetL = levelToDb(v.audioData.LeftLevel)
		targetR = levelToDb(v.audioData.RightLevel)
	}
	v.leftDb += vuSmoothingAlpha * (targetL - v.leftDb)
	v.rightDb += vuSmoothingAlpha * (targetR - v.rightDb)
}

func levelToDb(level float32) float64 {
	p := float64(level)
	if p < vuNoiseFloorPeak {
		p = vuNoiseFloorPeak
	}
	db := 20 * math.Log10(p)
	if db < vuDbMin {
		return vuDbMin
	}
	if db > vuDbMax {
		return vuDbMax
	}
	return db
}

func (v *VUMeter) View(width, height int) string {
	if !v.inited || width < 1 || height < 1 {
		return ""
	}

	// Layout 1: two dials side-by-side.
	sideW := (width - vuDialGap) / 2
	if sideW > vuMaxDialWidth {
		sideW = vuMaxDialWidth
	}
	sideH := height
	if sideH > vuMaxDialHeight {
		sideH = vuMaxDialHeight
	}
	if sideW >= vuSideBySideMinWidth && sideH >= vuMinDialHeight {
		left := renderTEACDial(sideW, sideH, v.leftDb, "LEFT")
		right := renderTEACDial(sideW, sideH, v.rightDb, "RIGHT")
		return composePair(left, right, sideW, sideH, vuDialGap, width, height)
	}

	// Layout 2: stacked — L on top, R below, each at the full pane width.
	stackW := width
	if stackW > vuMaxDialWidth {
		stackW = vuMaxDialWidth
	}
	stackH := (height - vuStackGap) / 2
	if stackH > vuMaxDialHeight {
		stackH = vuMaxDialHeight
	}
	if stackW >= vuMinDialWidth && stackH >= vuMinDialHeight {
		top := renderTEACDial(stackW, stackH, v.leftDb, "LEFT")
		bot := renderTEACDial(stackW, stackH, v.rightDb, "RIGHT")
		return composeStack(top, bot, stackW, stackH, vuStackGap, width, height)
	}

	// Last resort: stacked horizontal bars for terminals too small for
	// even a single full dial.
	return v.renderFallback(width, height)
}

// dbToAngleRad is a piecewise-linear map from dB to needle angle. The
// segment vuDbMin..vuDbPivot fills [-sweep, thetaZero]; the segment
// vuDbPivot..vuDbMax fills [thetaZero, +sweep]. Splitting at thetaZero
// gives the short positive-dB segment a wider angular share than a
// single linear map would.
func dbToAngleRad(db float64) float64 {
	sweep := vuSweepHalfDeg * math.Pi / 180.0
	thetaZero := vuThetaZeroFrac * sweep
	if db <= vuDbPivot {
		t := (db - vuDbMin) / (vuDbPivot - vuDbMin)
		return -sweep + (thetaZero-(-sweep))*t
	}
	t := (db - vuDbPivot) / (vuDbMax - vuDbPivot)
	return thetaZero + (sweep-thetaZero)*t
}

// vuKindLum returns the luminance step for a given kind. Arc is dim,
// tick is medium, needle is bright — matching the resting → peak
// gradient the band visualizers show.
func vuKindLum(k vuCellKind) float64 {
	switch k {
	case vuKindArc:
		return 0.35
	case vuKindTick:
		return 0.50
	case vuKindNeedle:
		return 0.60
	}
	return 0
}

// vuColumnHue returns the hue for a cell at (col, width), sweeping
// themeHueStart → themeHueStart + themeHueRange across the dial. This
// is the same range bandHue uses for spectrum/oscillogram bands.
func vuColumnHue(col, width int) float64 {
	var t float64
	if width > 1 {
		t = float64(col) / float64(width-1)
	}
	return themeHueStart + t*themeHueRange
}

// labelCell is one character of a tick number to overlay on the arc grid
// at a specific (row, col). The arc renderer skips its quadrant glyph
// whenever a labelCell is present at that position.
type labelCell struct {
	ch      byte
	r, g, b int
}

func renderTEACDial(width, height int, db float64, channel string) string {
	arcRows := height - vuBottomReserve
	if arcRows < 2 {
		arcRows = 2
	}
	subW := width * 2
	subH := arcRows * 2

	grid := make([]vuCellKind, subW*subH)

	pivotVX := float64(subW) / 2.0
	margin := float64(vuLabelEdgeMargin)
	sweep := vuSweepHalfDeg * math.Pi / 180.0

	// Size the arc to fill horizontally: half the arc width = pivotVX − margin.
	r := (pivotVX - margin) / math.Sin(sweep)

	// Place the pivot far below the visible area so the arc apex sits at
	// the chosen sub-row. The pivot is conceptually off-screen — only the
	// needle's outer segment is visible inside the grid.
	arcApexVy := float64(vuArcApexSubRow) * vuYAspect
	pivotVY := arcApexVy + r

	// If the curve would dip below the visible arc region at the edges,
	// shrink r so the edge points stay inside.
	maxEdgeVy := float64(subH) * vuYAspect
	edgeVy := pivotVY - r*math.Cos(sweep)
	if edgeVy > maxEdgeVy {
		rMax := (maxEdgeVy - arcApexVy) / (1 - math.Cos(sweep))
		if rMax < r && rMax > 0 {
			r = rMax
			pivotVY = arcApexVy + r
		}
	}
	if r < 4 {
		r = 4
	}

	ticks := teacTicks()

	stampArc(grid, subW, subH, pivotVX, pivotVY, r, sweep)

	// Every stalk projects to the same vertical height regardless of
	// theta — at the edges the radial extent grows by 1/cos(theta) to
	// compensate for the arc dipping. Result: all tick marks read as
	// the same short stroke that the middle ticks always had.
	stalkVyHeight := vuYAspect // 1 sub-row tall on every tick
	for _, t := range ticks {
		theta := dbToAngleRad(t.db)
		rrOuter := r + stalkVyHeight/math.Cos(theta)
		stampRadial(grid, subW, subH, pivotVX, pivotVY, r, rrOuter, theta, vuKindTick)
	}

	needleTipR := r * vuNeedleTipFrac
	needleBaseVy := arcApexVy + float64(arcRows)*vuYAspect*vuNeedleBaseFrac
	stampNeedle(grid, subW, subH, pivotVX, pivotVY, dbToAngleRad(db), needleTipR, needleBaseVy, vuKindNeedle)

	overlay := buildLabelOverlay(width, arcRows, ticks, pivotVX, pivotVY, r, stalkVyHeight)

	var buf strings.Builder
	buf.Grow(width*height*24 + 64)

	buf.WriteString(packArc(grid, subW, subH, width, arcRows, overlay))

	buf.WriteByte('\n')
	buf.WriteString(strings.Repeat(" ", width))
	buf.WriteByte('\n')
	buf.WriteString(centerTextGradient(width, channel))

	return buf.String()
}

// buildLabelOverlay places each labeled tick's text one cell row above
// that tick's own stalk top, so labels trace the same dip the arc does
// (highest at the apex, lowest at the edges). Ticks with an empty label
// contribute nothing here — only their stalk in the arc grid.
func buildLabelOverlay(width, arcRows int, ticks []tickDef, pivotVX, pivotVY, r, stalkVyHeight float64) map[int]labelCell {
	overlay := make(map[int]labelCell)

	set := func(row, col int, ch byte, cr, cg, cb int) {
		if col < 0 || col >= width || row < 0 || row >= arcRows {
			return
		}
		key := row*width + col
		if _, ok := overlay[key]; ok {
			return
		}
		overlay[key] = labelCell{ch: ch, r: cr, g: cg, b: cb}
	}

	for _, t := range ticks {
		if t.label == "" {
			continue
		}
		theta := dbToAngleRad(t.db)
		vx := pivotVX + r*math.Sin(theta)
		colArc := int(math.Round(vx / 2.0))

		vyTop := pivotVY - r*math.Cos(theta) - stalkVyHeight
		syTop := int(math.Round(vyTop / vuYAspect))
		row := syTop/2 - 2 // labels sit two cell rows above the stalk top
		if row < 0 {
			row = 0
		}

		start := colArc - len(t.label)/2
		if start+len(t.label) > width {
			start = width - len(t.label)
		}
		if start < 0 {
			start = 0
		}
		for i := 0; i < len(t.label); i++ {
			c := start + i
			cr, cg, cb := hslToRGB(vuColumnHue(c, width), 0.8, 0.55)
			set(row, c, t.label[i], cr, cg, cb)
		}
	}
	return overlay
}

// stampArc rasterizes the dial arc as a single sub-cell wide curve.
// Hue is derived from the cell column at render time so the arc shows
// the same green→purple gradient as the band visualizers.
func stampArc(grid []vuCellKind, subW, subH int, pivotVX, pivotVY, r, sweep float64) {
	steps := int(2*sweep*r) + 1
	if steps < 8 {
		steps = 8
	}
	for i := 0; i <= steps; i++ {
		t := float64(i) / float64(steps)
		theta := -sweep + 2*sweep*t
		plotVisualPx(grid, subW, subH, pivotVX, pivotVY, r, theta, vuKindArc)
	}
}

// stampRadial draws a single-angle radial segment between two radii.
func stampRadial(grid []vuCellKind, subW, subH int, pivotVX, pivotVY, rIn, rOut, theta float64, kind vuCellKind) {
	steps := int(math.Abs(rOut-rIn)*2) + 1
	if steps < 4 {
		steps = 4
	}
	for i := 0; i <= steps; i++ {
		rr := rIn + (rOut-rIn)*float64(i)/float64(steps)
		plotVisualPx(grid, subW, subH, pivotVX, pivotVY, rr, theta, kind)
	}
}

// stampNeedle traces a single-angle radial line from the rr at which
// vy equals baseVy (the visible base) out to tipR (the tip). The base
// clipping keeps the visible needle from extending all the way to the
// bottom of the grid.
func stampNeedle(grid []vuCellKind, subW, subH int, pivotVX, pivotVY, theta, tipR, baseVy float64, kind vuCellKind) {
	baseR := (pivotVY - baseVy) / math.Cos(theta)
	if baseR < 0 {
		baseR = 0
	}
	if baseR >= tipR {
		return
	}
	steps := int((tipR-baseR)*2) + 1
	if steps < 8 {
		steps = 8
	}
	for i := 0; i <= steps; i++ {
		rr := baseR + (tipR-baseR)*float64(i)/float64(steps)
		plotVisualPx(grid, subW, subH, pivotVX, pivotVY, rr, theta, kind)
	}
}

func plotVisualPx(grid []vuCellKind, subW, subH int, pivotVX, pivotVY, r, theta float64, kind vuCellKind) {
	vx := pivotVX + r*math.Sin(theta)
	vy := pivotVY - r*math.Cos(theta)
	sx := int(math.Round(vx))
	sy := int(math.Round(vy / vuYAspect))
	if sx < 0 || sx >= subW || sy < 0 || sy >= subH {
		return
	}
	if grid[sx+sy*subW] < kind {
		grid[sx+sy*subW] = kind
	}
}

// tickDef is one entry in the dial's fixed tick set. Every entry draws
// a stalk; only entries with a non-empty label also overlay a number.
type tickDef struct {
	db    float64
	label string
}

// teacTicks is the fixed tick set the dial always renders. Stalks
// appear at every dB position; only the extremes are labeled.
func teacTicks() []tickDef {
	return []tickDef{
		{db: -20, label: "-20"},
		{db: -10},
		{db: -7},
		{db: -5},
		{db: -3},
		{db: 0},
		{db: 1},
		{db: 2},
		{db: 3, label: "+3"},
	}
}

// centerTextGradient writes `text` centered within `width`, coloring
// each character by the vuColumnHue at its column. The result spans
// exactly `width` display cells.
func centerTextGradient(width int, text string) string {
	if len(text) >= width {
		text = text[:width]
	}
	pad := (width - len(text)) / 2
	var bld strings.Builder
	for i := 0; i < pad; i++ {
		bld.WriteByte(' ')
	}
	for i := 0; i < len(text); i++ {
		c := pad + i
		cr, cg, cb := hslToRGB(vuColumnHue(c, width), 0.8, 0.55)
		writeAnsiFg(&bld, cr, cg, cb)
		bld.WriteByte(text[i])
		bld.WriteString(ansiReset)
	}
	for i := pad + len(text); i < width; i++ {
		bld.WriteByte(' ')
	}
	return bld.String()
}

func packArc(grid []vuCellKind, subW, subH, width, arcRows int, overlay map[int]labelCell) string {
	var buf strings.Builder
	buf.Grow(width * arcRows * 20)
	for row := 0; row < arcRows; row++ {
		for col := 0; col < width; col++ {
			if lc, ok := overlay[row*width+col]; ok {
				writeAnsiFg(&buf, lc.r, lc.g, lc.b)
				buf.WriteByte(lc.ch)
				buf.WriteString(ansiReset)
				continue
			}
			sxL := col * 2
			syT := row * 2
			tl := grid[sxL+syT*subW]
			tr := grid[(sxL+1)+syT*subW]
			bl := grid[sxL+(syT+1)*subW]
			br := grid[(sxL+1)+(syT+1)*subW]
			if tl == vuKindEmpty && tr == vuKindEmpty && bl == vuKindEmpty && br == vuKindEmpty {
				buf.WriteByte(' ')
				continue
			}
			pattern := 0
			if tl != vuKindEmpty {
				pattern |= quadrantBits[0]
			}
			if tr != vuKindEmpty {
				pattern |= quadrantBits[1]
			}
			if bl != vuKindEmpty {
				pattern |= quadrantBits[2]
			}
			if br != vuKindEmpty {
				pattern |= quadrantBits[3]
			}
			best := tl
			if tr > best {
				best = tr
			}
			if bl > best {
				best = bl
			}
			if br > best {
				best = br
			}
			cr, cg, cb := hslToRGB(vuColumnHue(col, width), 0.8, vuKindLum(best))
			writeAnsiFg(&buf, cr, cg, cb)
			buf.WriteString(quadrantGlyphs[pattern])
			buf.WriteString(ansiReset)
		}
		if row < arcRows-1 {
			buf.WriteByte('\n')
		}
	}
	return buf.String()
}

// composePair joins two dials horizontally and centers the result in
// the pane. Each dial is dialH display-lines tall and dialW cells wide;
// ANSI escapes inside the line don't change those counts.
func composePair(left, right string, dialW, dialH, gap, paneW, paneH int) string {
	leftLines := splitFillLines(left, dialW, dialH)
	rightLines := splitFillLines(right, dialW, dialH)
	gapStr := strings.Repeat(" ", gap)
	content := make([]string, dialH)
	for i := 0; i < dialH; i++ {
		content[i] = leftLines[i] + gapStr + rightLines[i]
	}
	return centerInPane(content, dialW*2+gap, paneW, paneH)
}

// composeStack joins two dials vertically (top over bot) with a `gap`
// blank rows between them and centers the result in the pane.
func composeStack(top, bot string, dialW, dialH, gap, paneW, paneH int) string {
	topLines := splitFillLines(top, dialW, dialH)
	botLines := splitFillLines(bot, dialW, dialH)
	blank := strings.Repeat(" ", dialW)
	content := make([]string, 0, dialH*2+gap)
	content = append(content, topLines...)
	for i := 0; i < gap; i++ {
		content = append(content, blank)
	}
	content = append(content, botLines...)
	return centerInPane(content, dialW, paneW, paneH)
}

// splitFillLines splits s on newlines and pads the result up to `h`
// lines using a `w`-wide blank line. Used to normalize a rendered dial
// before composition.
func splitFillLines(s string, w, h int) []string {
	lines := strings.Split(s, "\n")
	if len(lines) >= h {
		return lines
	}
	blank := strings.Repeat(" ", w)
	for len(lines) < h {
		lines = append(lines, blank)
	}
	return lines
}

// centerInPane places `content` (each line `contentW` display-cells
// wide) centered inside (paneW, paneH), padding all four sides with
// spaces. Returns exactly paneH lines joined by '\n'.
func centerInPane(content []string, contentW, paneW, paneH int) string {
	leftPad := max0((paneW - contentW) / 2)
	rightPad := max0(paneW - contentW - leftPad)
	topPad := max0((paneH - len(content)) / 2)
	botPad := max0(paneH - len(content) - topPad)

	var b strings.Builder
	b.Grow(paneW*paneH*4 + 64)
	blank := strings.Repeat(" ", paneW)
	leftPadStr := strings.Repeat(" ", leftPad)
	rightPadStr := strings.Repeat(" ", rightPad)

	written := 0
	emit := func(line string) {
		b.WriteString(line)
		written++
		if written < paneH {
			b.WriteByte('\n')
		}
	}
	for i := 0; i < topPad; i++ {
		emit(blank)
	}
	for _, line := range content {
		emit(leftPadStr + line + rightPadStr)
	}
	for i := 0; i < botPad; i++ {
		emit(blank)
	}
	return b.String()
}

func max0(v int) int {
	if v < 0 {
		return 0
	}
	return v
}

// renderFallback is shown when the pane is too small for even one full
// dial: two horizontal bars labeled LEFT and RIGHT, colored with the
// same column-driven green→purple gradient as the dial.
func (v *VUMeter) renderFallback(width, height int) string {
	var b strings.Builder
	b.Grow(width*height*8 + 32)
	rowL := height / 4
	rowR := height - 1 - height/4
	if rowR <= rowL {
		rowR = rowL + 1
		if rowR >= height {
			rowR = height - 1
		}
	}
	for row := 0; row < height; row++ {
		switch row {
		case rowL:
			b.WriteString(renderBarRow(width, v.leftDb, "LEFT"))
		case rowR:
			b.WriteString(renderBarRow(width, v.rightDb, "RIGHT"))
		default:
			for c := 0; c < width; c++ {
				b.WriteByte(' ')
			}
		}
		if row < height-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func renderBarRow(width int, db float64, label string) string {
	if width < 4 {
		var b strings.Builder
		for i := 0; i < width; i++ {
			b.WriteByte(' ')
		}
		return b.String()
	}
	prefix := label + " "
	barW := width - len(prefix)
	t := (db - vuDbMin) / (vuDbMax - vuDbMin)
	if t < 0 {
		t = 0
	} else if t > 1 {
		t = 1
	}

	var b strings.Builder
	// Prefix characters take the gradient hue of their column.
	for i := 0; i < len(prefix); i++ {
		cr, cg, cb := hslToRGB(vuColumnHue(i, width), 0.8, 0.55)
		writeAnsiFg(&b, cr, cg, cb)
		b.WriteByte(prefix[i])
		b.WriteString(ansiReset)
	}
	for c := 0; c < barW; c++ {
		var colT float64
		if barW > 1 {
			colT = float64(c) / float64(barW-1)
		}
		if colT > t {
			b.WriteByte(' ')
			continue
		}
		cr, cg, cb := hslToRGB(vuColumnHue(len(prefix)+c, width), 0.8, 0.50)
		writeAnsiFg(&b, cr, cg, cb)
		b.WriteString("█")
		b.WriteString(ansiReset)
	}
	return b.String()
}
