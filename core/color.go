package core

// Dim returns c blended toward the terminal's ground by amount (0..1): darker on dark
// backgrounds, lighter on light ones, so a dimmed accent is quieter on both. Colors are
// ANSI-256 indexes, so each is expanded to RGB, blended and snapped back.
func Dim(c Color, amount float64) Color {
	return Color{
		Light: dimIndex(c.Light, 255, amount),
		Dark:  dimIndex(c.Dark, 0, amount),
	}
}

// dimIndex blends one index toward ground (0 black, 255 white) and returns the nearest
// index, stepping the blend up until the index actually changes (the cube is coarse). A
// color already at the ground is returned unchanged.
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

// ansiBasic is xterm's default 0..15. Terminals redefine these, so they are expanded as
// inputs but never chosen as outputs.
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

// nearestANSI returns the closest index in 16..255 to (r, g, b); the basic sixteen are
// excluded because their real colors are unknown.
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
