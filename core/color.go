package core

// Dim returns c one step quieter: blended toward the terminal's own ground, which means
// DARKER on a dark background and LIGHTER on a light one. Both variants are dimmed, each
// toward its own ground (Light toward white, Dark toward black), so a dimmed accent reads
// as subordinate to the undimmed one under EITHER background — a literal darken would make
// it the louder of the two on a light terminal, inverting the hierarchy it exists to
// express. amount is the fraction of the distance to the ground, 0..1.
//
// The palette is ANSI-256 indices rather than hex (see Color), so this is not a plain
// multiply: the index is expanded to RGB, blended, then snapped back to the nearest index.
func Dim(c Color, amount float64) Color {
	return Color{
		Light: dimIndex(c.Light, 255, amount),
		Dark:  dimIndex(c.Dark, 0, amount),
	}
}

// dimIndex blends one ANSI-256 index toward ground (0 black, 255 white) and returns the
// nearest index to the result. Quantizing can land back on the input — the 6×6×6 cube is
// coarse and a small blend need not leave the cell — which would make Dim a silent no-op
// on some themes, so the blend is stepped up until the index actually moves. A color
// already sitting on the ground never moves and comes back as it went in.
func dimIndex(i uint8, ground int, amount float64) uint8 {
	r, g, b := ansiRGB(i)
	for a := amount; a <= 1.0001; a += 0.05 {
		n := nearestANSI(blend(r, ground, a), blend(g, ground, a), blend(b, ground, a))
		if n != i {
			return n
		}
	}
	return i
}

func blend(c, ground int, amount float64) int {
	return c + int(float64(ground-c)*amount)
}

// ansiCube is the 6×6×6 color cube's per-channel levels, shared by the 16..231 expansion
// and the reverse search.
var ansiCube = [6]int{0, 95, 135, 175, 215, 255}

// ansiBasic is the xterm default for indices 0..15. Those sixteen are the ones a terminal
// theme is free to redefine, so they are expanded (an input can be one) but never chosen
// as an output — see nearestANSI.
var ansiBasic = [16][3]int{
	{0, 0, 0}, {128, 0, 0}, {0, 128, 0}, {128, 128, 0},
	{0, 0, 128}, {128, 0, 128}, {0, 128, 128}, {192, 192, 192},
	{128, 128, 128}, {255, 0, 0}, {0, 255, 0}, {255, 255, 0},
	{0, 0, 255}, {255, 0, 255}, {0, 255, 255}, {255, 255, 255},
}

// ansiRGB expands an ANSI-256 index to its RGB: the basic sixteen from the table, 16..231
// from the cube, and 232..255 from the greyscale ramp.
func ansiRGB(idx uint8) (int, int, int) {
	i := int(idx)
	switch {
	case i < 16:
		c := ansiBasic[i]
		return c[0], c[1], c[2]
	case i < 232:
		i -= 16
		return ansiCube[i/36], ansiCube[(i/6)%6], ansiCube[i%6]
	default:
		v := 8 + 10*(i-232)
		return v, v, v
	}
}

// nearestANSI is the reverse: the index whose RGB is closest to (r,g,b) by squared
// distance. Only 16..255 are candidates — the basic sixteen are whatever the user's
// terminal profile says they are, so picking one would make the dim's result depend on a
// palette this code cannot see.
func nearestANSI(r, g, b int) uint8 {
	best, bestDist := 16, 1<<31-1
	for i := 16; i < 256; i++ {
		cr, cg, cb := ansiRGB(uint8(i))
		d := (cr-r)*(cr-r) + (cg-g)*(cg-g) + (cb-b)*(cb-b)
		if d < bestDist {
			best, bestDist = i, d
		}
	}
	return uint8(best)
}
