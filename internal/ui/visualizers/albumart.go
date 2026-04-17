package visualizers

import (
	"hash/fnv"
	"image"
	"image/color"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// dissolveFrames is the number of animation frames for the album art dissolve effect (~5 s at 30 fps).
const dissolveFrames = 150

type AlbumArt struct {
	img      image.Image
	hasImage bool

	numBlocks int
	pixels    []int32
	resolveAt []int

	frame       int
	totalFrames int
	resolved    bool

	rng    uint64
	inited bool
}

func NewAlbumArt() *AlbumArt {
	return &AlbumArt{}
}

func (a *AlbumArt) Init(seed string, durationMs int) {
	h := fnv.New64a()
	h.Write([]byte(seed))
	a.rng = h.Sum64()
	a.frame = 0
	a.totalFrames = 0
	a.resolved = false
	a.hasImage = false
	a.img = nil
	a.numBlocks = 0
	a.inited = true
}

func (a *AlbumArt) SetImage(img image.Image) {
	a.img = img
	a.hasImage = true
	a.frame = 0
	a.totalFrames = 0
	a.resolved = false
	a.numBlocks = 0
}

func (a *AlbumArt) Advance() {
	if !a.inited {
		return
	}
	if a.hasImage && !a.resolved && a.totalFrames > 0 {
		a.frame++
		if a.frame >= a.totalFrames {
			a.resolved = true
		}
	}
}

func (a *AlbumArt) View(width, height int) string {
	if !a.inited || width < 1 || height < 1 {
		return ""
	}

	pixW := width
	pixH := height * 2

	numBlocks := pixW
	if pixH < numBlocks {
		numBlocks = pixH
	}
	if numBlocks < 1 {
		return ""
	}

	if numBlocks != a.numBlocks {
		a.computeGrid(numBlocks)
	}

	offsetX := (pixW - numBlocks) / 2
	offsetY := (pixH - numBlocks) / 2

	var bgColor int32
	if lipgloss.HasDarkBackground() {
		bgColor = 0
	} else {
		bgColor = 0xFF<<16 | 0xFF<<8 | 0xFF
	}

	var buf strings.Builder
	buf.Grow(width * height * 20)

	for row := range height {
		for col := range width {
			topC := a.colorAt(col, row*2, offsetX, offsetY, bgColor)
			botC := a.colorAt(col, row*2+1, offsetX, offsetY, bgColor)

			if topC < 0 && botC < 0 {
				buf.WriteRune(' ')
			} else {
				if topC < 0 {
					topC = bgColor
				}
				if botC < 0 {
					botC = bgColor
				}
				writeAnsiFgBg(&buf,
					int(topC>>16&0xFF), int(topC>>8&0xFF), int(topC&0xFF),
					int(botC>>16&0xFF), int(botC>>8&0xFF), int(botC&0xFF),
				)
				buf.WriteRune('▀')
				buf.WriteString(ansiReset)
			}
		}
		if row < height-1 {
			buf.WriteRune('\n')
		}
	}

	return buf.String()
}

func (a *AlbumArt) colorAt(px, py, offsetX, offsetY int, bgColor int32) int32 {
	n := a.numBlocks
	relX := px - offsetX
	relY := py - offsetY

	if relX < 0 || relX >= n || relY < 0 || relY >= n {
		return -1
	}

	idx := relY*n + relX
	if a.hasImage && a.frame >= a.resolveAt[idx] {
		return a.pixels[idx]
	}
	return bgColor
}

func (a *AlbumArt) computeGrid(numBlocks int) {
	a.numBlocks = numBlocks
	a.frame = 0
	a.resolved = false
	total := numBlocks * numBlocks
	a.totalFrames = dissolveFrames

	a.pixels = make([]int32, total)
	if a.hasImage && a.img != nil {
		bounds := a.img.Bounds()
		imgW := bounds.Dx()
		imgH := bounds.Dy()

		for by := range numBlocks {
			for bx := range numBlocks {
				srcX := bounds.Min.X + (bx*imgW+imgW/2)/numBlocks
				srcY := bounds.Min.Y + (by*imgH+imgH/2)/numBlocks
				if srcX >= bounds.Max.X {
					srcX = bounds.Max.X - 1
				}
				if srcY >= bounds.Max.Y {
					srcY = bounds.Max.Y - 1
				}
				r, g, b, _ := a.img.At(srcX, srcY).RGBA()
				a.pixels[by*numBlocks+bx] = int32(r>>8)<<16 | int32(g>>8)<<8 | int32(b>>8)
			}
		}
	}

	// Fisher-Yates shuffle for resolve order.
	order := make([]int, total)
	for i := range total {
		order[i] = i
	}
	rng := a.rng
	for i := total - 1; i > 0; i-- {
		rng = xorshift(rng)
		j := int(rng % uint64(i+1))
		order[i], order[j] = order[j], order[i]
	}

	a.resolveAt = make([]int, total)
	for k, blockIdx := range order {
		a.resolveAt[blockIdx] = k * a.totalFrames / total
	}
}

func MusicNoteFallback() image.Image {
	const size = 16
	img := image.NewRGBA(image.Rect(0, 0, size, size))

	note := []string{
		"                ",
		"       ####     ",
		"       #####    ",
		"       #  ##    ",
		"       #        ",
		"       #        ",
		"       #        ",
		"       #        ",
		"       #        ",
		"       #        ",
		"     ###        ",
		"    ####        ",
		"    ####        ",
		"     ##         ",
		"                ",
		"                ",
	}

	c := color.RGBA{R: 139, G: 92, B: 246, A: 255}
	for y, row := range note {
		for x, ch := range row {
			if ch == '#' {
				img.Set(x, y, c)
			}
		}
	}

	return img
}
