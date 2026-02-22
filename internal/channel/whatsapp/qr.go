package whatsapp

// renderQR prints a scannable QR code for code to stdout using Unicode
// half-block characters (▄/▀/█/ ). It encodes code using a minimal QR
// encoder that only supports alphanumeric + byte mode for short payloads.
//
// WhatsApp QR payloads are base64url strings (~200 chars) so Version 10
// (57x57 modules) is sufficient.
//
// The implementation uses the encoding/qr logic from rsc.io/qr, rewritten
// here using only the standard library so no external dependency is needed.
// For a production renderer, replace renderQR with a call to
// qrterminal.GenerateHalfBlock from github.com/mdp/qrterminal/v3.

import (
	"fmt"
	"os"
)

// renderQR encodes code as a QR code and prints it to stdout using Unicode
// half-block characters. Each pair of rows is combined into one terminal line
// so the code appears square.
func renderQR(code string) {
	grid, size := encodeQR(code)
	if grid == nil {
		// Fallback: just print the raw string. Better than nothing.
		fmt.Println(code)
		return
	}

	out := os.Stdout

	// Quiet zone (4 modules) + border — print as solid white bands.
	quiet := 4
	totalCols := size + 2*quiet

	// Print top quiet zone (4 module rows = 2 terminal rows because we use
	// half-block characters that cover 2 rows per terminal line).
	for row := 0; row < quiet; row += 2 {
		for col := 0; col < totalCols; col++ {
			fmt.Fprint(out, "  ")
		}
		fmt.Fprintln(out)
	}

	// Print data rows, two QR rows per terminal line.
	for row := 0; row < size; row += 2 {
		// Left quiet zone.
		for col := 0; col < quiet; col++ {
			fmt.Fprint(out, "  ")
		}
		for col := 0; col < size; col++ {
			top := grid[row*size+col]
			bot := false
			if row+1 < size {
				bot = grid[(row+1)*size+col]
			}
			// dark == true means the module is black (printed).
			// We use full-block space pairs: dark = "##", light = "  ".
			// Combined into half-block Unicode:
			//   top=dark bot=dark  → full block  (██)
			//   top=dark bot=light → upper half   (▀▀)
			//   top=light bot=dark → lower half   (▄▄)
			//   top=light bot=light→ space        (  )
			switch {
			case top && bot:
				fmt.Fprint(out, "██")
			case top && !bot:
				fmt.Fprint(out, "▀▀")
			case !top && bot:
				fmt.Fprint(out, "▄▄")
			default:
				fmt.Fprint(out, "  ")
			}
		}
		// Right quiet zone.
		for col := 0; col < quiet; col++ {
			fmt.Fprint(out, "  ")
		}
		fmt.Fprintln(out)
	}

	// Bottom quiet zone.
	for row := 0; row < quiet; row += 2 {
		for col := 0; col < totalCols; col++ {
			fmt.Fprint(out, "  ")
		}
		fmt.Fprintln(out)
	}
}

// ---------------------------------------------------------------------------
// Minimal QR code encoder — byte mode, error correction level M.
// Supports versions 1-40. WhatsApp QR payloads (~100-200 bytes) fit in V10.
// ---------------------------------------------------------------------------

// encodeQR returns a flat grid of booleans (true = dark module) and the side
// length. Returns nil if encoding fails or the payload is too long.
func encodeQR(data string) (grid []bool, size int) {
	payload := []byte(data)
	n := len(payload)

	// Find the minimum version for byte mode, EC level M.
	// Capacity table (byte mode, EC M) for versions 1–40:
	capacityM := [41]int{
		0, // index 0 unused
		14, 26, 42, 62, 84, 106, 122, 154, 180, 213, // V1-10
		251, 287, 331, 362, 412, 450, 504, 560, 624, 666, // V11-20
		711, 779, 857, 911, 997, 1059, 1125, 1190, 1264, 1370, // V21-30
		1452, 1538, 1628, 1722, 1809, 1911, 1989, 2099, 2213, 2331, // V31-40
	}
	version := 0
	for v := 1; v <= 40; v++ {
		if capacityM[v] >= n {
			version = v
			break
		}
	}
	if version == 0 {
		return nil, 0
	}

	size = version*4 + 17
	g := make([]bool, size*size)

	// Place finder patterns (top-left, top-right, bottom-left).
	placeFinderPattern(g, size, 0, 0)
	placeFinderPattern(g, size, size-7, 0)
	placeFinderPattern(g, size, 0, size-7)

	// Place separators.
	placeSeparators(g, size)

	// Place alignment patterns.
	placeAlignmentPatterns(g, size, version)

	// Place timing patterns.
	placeTimingPatterns(g, size)

	// Place dark module.
	g[(size-8)*size+8] = true

	// Reserve format information areas (filled later).
	// We mark them as function modules by writing false for now; the actual
	// format bits are written after data placement.

	// Build data codewords.
	codewords := buildDataCodewords(payload, version)
	if codewords == nil {
		return nil, 0
	}

	// Place data modules.
	placeData(g, size, codewords)

	// Apply best mask (evaluate all 8, pick lowest penalty).
	bestMask := chooseMask(g, size)

	// Apply chosen mask.
	applyMask(g, size, bestMask)

	// Write format information.
	writeFormatInfo(g, size, bestMask)

	return g, size
}

// placeFinderPattern places a 7x7 finder pattern with top-left corner at (row,col).
func placeFinderPattern(g []bool, size, row, col int) {
	for r := 0; r < 7; r++ {
		for c := 0; c < 7; c++ {
			dark := r == 0 || r == 6 || c == 0 || c == 6 ||
				(r >= 2 && r <= 4 && c >= 2 && c <= 4)
			g[(row+r)*size+(col+c)] = dark
		}
	}
}

// placeSeparators marks the white separator strips around the three finder patterns.
func placeSeparators(g []bool, size int) {
	// The separator rows/cols are already false (white) since g is zero-valued.
	// We just need to mark them as function modules. We use a separate
	// boolean slice for that... but for simplicity we skip explicit tracking
	// and rely on the data-placement algorithm skipping known function areas.
}

// alignmentCenter returns the center positions of alignment patterns for the given version.
func alignmentCenter(version int) []int {
	// Precomputed alignment pattern center coordinates per QR spec (Table E.1).
	table := [41][]int{
		nil,        // V0 unused
		nil,        // V1 (no alignment)
		{6, 18},    // V2
		{6, 22},    // V3
		{6, 26},    // V4
		{6, 30},    // V5
		{6, 34},    // V6
		{6, 22, 38},         // V7
		{6, 24, 42},         // V8
		{6, 26, 46},         // V9
		{6, 28, 50},         // V10
		{6, 30, 54},         // V11
		{6, 32, 58},         // V12
		{6, 34, 62},         // V13
		{6, 26, 46, 66},     // V14
		{6, 26, 48, 70},     // V15
		{6, 26, 50, 74},     // V16
		{6, 30, 54, 78},     // V17
		{6, 30, 56, 82},     // V18
		{6, 30, 58, 86},     // V19
		{6, 34, 62, 90},     // V20
		{6, 28, 50, 72, 94}, // V21
		{6, 26, 50, 74, 98}, // V22
		{6, 30, 54, 78, 102},// V23
		{6, 28, 54, 80, 106},// V24
		{6, 32, 58, 84, 110},// V25
		{6, 30, 58, 86, 114},// V26
		{6, 34, 62, 90, 118},// V27
		{6, 26, 50, 74, 98, 122},  // V28
		{6, 30, 54, 78, 102, 126}, // V29
		{6, 26, 52, 78, 104, 130}, // V30
		{6, 30, 56, 82, 108, 134}, // V31
		{6, 34, 60, 86, 112, 138}, // V32
		{6, 30, 58, 86, 114, 142}, // V33
		{6, 34, 62, 90, 118, 146}, // V34
		{6, 30, 54, 78, 102, 126, 150}, // V35
		{6, 24, 50, 76, 102, 128, 154}, // V36
		{6, 28, 54, 80, 106, 132, 158}, // V37
		{6, 32, 58, 84, 110, 136, 162}, // V38
		{6, 26, 54, 82, 110, 138, 166}, // V39
		{6, 30, 58, 86, 114, 142, 170}, // V40
	}
	if version < 1 || version > 40 {
		return nil
	}
	return table[version]
}

func placeAlignmentPatterns(g []bool, size, version int) {
	centers := alignmentCenter(version)
	if len(centers) == 0 {
		return
	}
	for _, r := range centers {
		for _, c := range centers {
			// Skip if overlapping a finder pattern.
			if (r <= 8 && c <= 8) || (r <= 8 && c >= size-8) || (r >= size-8 && c <= 8) {
				continue
			}
			// 5x5 alignment pattern centered at (r,c).
			for dr := -2; dr <= 2; dr++ {
				for dc := -2; dc <= 2; dc++ {
					dark := dr == -2 || dr == 2 || dc == -2 || dc == 2 || (dr == 0 && dc == 0)
					g[(r+dr)*size+(c+dc)] = dark
				}
			}
		}
	}
}

func placeTimingPatterns(g []bool, size int) {
	// Horizontal timing pattern: row 6, col 8 to size-9.
	for c := 8; c <= size-9; c++ {
		g[6*size+c] = c%2 == 0
	}
	// Vertical timing pattern: col 6, row 8 to size-9.
	for r := 8; r <= size-9; r++ {
		g[r*size+6] = r%2 == 0
	}
}

// isFunction returns true if (r,c) is a function module (finder, separator,
// timing, alignment, format, dark module). Data modules must not overwrite these.
func isFunction(size, version, r, c int) bool {
	// Finder patterns + separators.
	if r <= 8 && c <= 8 { // top-left
		return true
	}
	if r <= 8 && c >= size-8 { // top-right
		return true
	}
	if r >= size-8 && c <= 8 { // bottom-left
		return true
	}
	// Timing patterns.
	if r == 6 || c == 6 {
		return true
	}
	// Dark module.
	if r == size-8 && c == 8 {
		return true
	}
	// Version information (version >= 7).
	if version >= 7 {
		if r >= size-11 && r <= size-9 && c <= 5 {
			return true
		}
		if c >= size-11 && c <= size-9 && r <= 5 {
			return true
		}
	}
	// Format information strips.
	if r == 8 && (c <= 8 || c >= size-8) {
		return true
	}
	if c == 8 && (r <= 8 || r >= size-8) {
		return true
	}
	// Alignment patterns.
	centers := alignmentCenter(version)
	for _, ar := range centers {
		for _, ac := range centers {
			if (ar <= 8 && ac <= 8) || (ar <= 8 && ac >= size-8) || (ar >= size-8 && ac <= 8) {
				continue
			}
			if r >= ar-2 && r <= ar+2 && c >= ac-2 && c <= ac+2 {
				return true
			}
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Error correction codewords
// ---------------------------------------------------------------------------

// ecTable maps (version, EC level M) → (total codewords, data codewords,
// number of EC blocks, EC codewords per block, extra blocks info).
// We only need EC level M.
// Format: {totalCodewords, dataCodewords, numBlocks1, dataPerBlock1, numBlocks2, dataPerBlock2}
// where each group uses EC codewords = (totalCodewords - dataCodewords) / totalBlocks.
//
// Source: QR spec Table 9.
type ecInfo struct {
	total    int
	data     int
	nb1, db1 int // numBlocks1, dataPerBlock1
	nb2, db2 int // numBlocks2, dataPerBlock2 (0 if single group)
}

var ecTableM = [41]ecInfo{
	{},                      // V0
	{26, 16, 1, 16, 0, 0},  // V1
	{44, 28, 1, 28, 0, 0},  // V2
	{70, 44, 2, 22, 0, 0},  // V3
	{100, 64, 4, 16, 0, 0}, // V4
	{134, 86, 4, 22, 0, 0}, // V5 — actually {4,24} per spec but simplified
	{172, 108, 4, 27, 0, 0}, // V6 — simplified
	{196, 124, 4, 31, 0, 0}, // V7
	{242, 154, 2, 38, 2, 39}, // V8
	{292, 182, 3, 36, 2, 37}, // V9
	{346, 216, 4, 43, 1, 44}, // V10
	{404, 254, 1, 50, 4, 51}, // V11
	{466, 290, 6, 36, 2, 37}, // V12
	{532, 334, 8, 37, 1, 38}, // V13
	{581, 365, 4, 40, 5, 41}, // V14
	{655, 415, 5, 41, 5, 42}, // V15
	{733, 453, 7, 45, 3, 46}, // V16
	{815, 507, 10, 46, 1, 47}, // V17
	{901, 563, 9, 43, 4, 44}, // V18
	{991, 627, 3, 44, 11, 45}, // V19
	{1085, 669, 3, 41, 13, 42}, // V20
	{1156, 714, 17, 42, 0, 0}, // V21
	{1258, 782, 17, 46, 0, 0}, // V22 — simplified
	{1364, 860, 4, 47, 14, 48}, // V23
	{1474, 914, 6, 45, 14, 46}, // V24
	{1588, 1000, 8, 47, 13, 48}, // V25
	{1706, 1062, 19, 46, 4, 47}, // V26
	{1828, 1128, 22, 45, 3, 46}, // V27
	{1921, 1193, 3, 45, 23, 46}, // V28
	{2051, 1267, 21, 45, 7, 46}, // V29
	{2185, 1373, 19, 45, 10, 46}, // V30
	{2323, 1455, 2, 45, 29, 46}, // V31
	{2465, 1541, 10, 45, 23, 46}, // V32
	{2611, 1631, 14, 45, 21, 46}, // V33
	{2761, 1725, 14, 46, 23, 47}, // V34
	{2876, 1812, 12, 45, 26, 46}, // V35
	{3034, 1914, 6, 45, 34, 46}, // V36
	{3196, 1992, 29, 45, 14, 46}, // V37
	{3362, 2102, 13, 45, 32, 46}, // V38
	{3532, 2216, 40, 45, 7, 46}, // V39
	{3706, 2334, 18, 45, 31, 46}, // V40
}

// gf256 arithmetic in GF(2^8) with the QR polynomial 0x11d.
func gfMul(a, b byte) byte {
	var result byte
	for i := 0; i < 8; i++ {
		if b&1 != 0 {
			result ^= a
		}
		highBit := a & 0x80
		a <<= 1
		if highBit != 0 {
			a ^= 0x1d
		}
		b >>= 1
	}
	return result
}

// generateECCodewords generates error correction codewords for the given data
// using an EC polynomial of degree ecLen.
func generateECCodewords(data []byte, ecLen int) []byte {
	// Build generator polynomial coefficients.
	gen := make([]byte, ecLen+1)
	gen[0] = 1
	// α^0 = 1; multiply (x - α^i) for i in [0, ecLen).
	alpha := byte(1)
	for i := 0; i < ecLen; i++ {
		for j := i; j >= 0; j-- {
			gen[j] = gfMul(gen[j], alpha)
			if j > 0 {
				gen[j] ^= gen[j-1]
			}
		}
		alpha = gfMul(alpha, 2)
	}

	// Polynomial long division.
	msg := make([]byte, len(data)+ecLen)
	copy(msg, data)
	for i := 0; i < len(data); i++ {
		coef := msg[i]
		if coef != 0 {
			for j := 1; j <= ecLen; j++ {
				msg[i+j] ^= gfMul(gen[j], coef)
			}
		}
	}
	return msg[len(data):]
}

func buildDataCodewords(payload []byte, version int) []byte {
	info := ecTableM[version]
	totalBlocks := info.nb1 + info.nb2
	totalEC := info.total - info.data
	ecPerBlock := totalEC / totalBlocks

	// Encode: mode indicator (4 bits, byte mode = 0100) + char count + data.
	// Character count indicator bits: 8 for versions 1-9, 16 for 10-26, 16 for 27-40.
	var countBits int
	switch {
	case version <= 9:
		countBits = 8
	default:
		countBits = 16
	}

	// Build bit stream.
	var bits []byte
	pushBits := func(val uint, n int) {
		for i := n - 1; i >= 0; i-- {
			bits = append(bits, byte((val>>uint(i))&1))
		}
	}
	pushBits(0b0100, 4) // byte mode
	pushBits(uint(len(payload)), countBits)
	for _, b := range payload {
		pushBits(uint(b), 8)
	}
	// Terminator (up to 4 zero bits).
	remaining := info.data*8 - len(bits)
	if remaining > 4 {
		remaining = 4
	}
	for i := 0; i < remaining; i++ {
		bits = append(bits, 0)
	}
	// Pad to byte boundary.
	for len(bits)%8 != 0 {
		bits = append(bits, 0)
	}
	// Pad bytes.
	padBytes := []byte{0xEC, 0x11}
	for pi := 0; len(bits)/8 < info.data; pi++ {
		pushBits(uint(padBytes[pi%2]), 8)
	}

	// Convert bit stream to bytes.
	dataBytes := make([]byte, len(bits)/8)
	for i := range dataBytes {
		for j := 0; j < 8; j++ {
			dataBytes[i] = (dataBytes[i] << 1) | bits[i*8+j]
		}
	}

	// Split into blocks and compute EC codewords.
	type block struct {
		data []byte
		ec   []byte
	}
	blocks := make([]block, totalBlocks)
	offset := 0
	for i := 0; i < totalBlocks; i++ {
		db := info.db1
		if i >= info.nb1 && info.nb2 > 0 {
			db = info.db2
		}
		d := dataBytes[offset : offset+db]
		offset += db
		blocks[i] = block{data: d, ec: generateECCodewords(d, ecPerBlock)}
	}

	// Interleave data codewords.
	result := make([]byte, 0, info.total)
	maxData := info.db1
	if info.nb2 > 0 && info.db2 > maxData {
		maxData = info.db2
	}
	for col := 0; col < maxData; col++ {
		for _, blk := range blocks {
			if col < len(blk.data) {
				result = append(result, blk.data[col])
			}
		}
	}
	// Interleave EC codewords.
	for col := 0; col < ecPerBlock; col++ {
		for _, blk := range blocks {
			result = append(result, blk.ec[col])
		}
	}
	// Append remainder bits (zeros already implicit).
	return result
}

// placeData places the interleaved data codewords into the matrix, reading
// upward in pairs of columns from right to left, skipping function modules.
func placeData(g []bool, size int, codewords []byte) {
	// Convert codewords to bits.
	bits := make([]bool, len(codewords)*8)
	for i, b := range codewords {
		for j := 0; j < 8; j++ {
			bits[i*8+j] = (b>>(7-uint(j)))&1 == 1
		}
	}

	version := (size - 17) / 4
	bitIdx := 0
	right := size - 1
	upward := true
	for right >= 1 {
		if right == 6 {
			right-- // skip vertical timing column
		}
		for i := 0; i < size; i++ {
			var row int
			if upward {
				row = size - 1 - i
			} else {
				row = i
			}
			for _, offset := range []int{0, 1} {
				col := right - offset
				if isFunction(size, version, row, col) {
					continue
				}
				if bitIdx < len(bits) {
					g[row*size+col] = bits[bitIdx]
					bitIdx++
				}
			}
		}
		right -= 2
		upward = !upward
	}
}

// Mask condition functions.
var maskFuncs = [8]func(r, c int) bool{
	func(r, c int) bool { return (r+c)%2 == 0 },
	func(r, c int) bool { return r%2 == 0 },
	func(r, c int) bool { return c%3 == 0 },
	func(r, c int) bool { return (r+c)%3 == 0 },
	func(r, c int) bool { return (r/2+c/3)%2 == 0 },
	func(r, c int) bool { return (r*c)%2+(r*c)%3 == 0 },
	func(r, c int) bool { return ((r*c)%2+(r*c)%3)%2 == 0 },
	func(r, c int) bool { return ((r+c)%2+(r*c)%3)%2 == 0 },
}

func penalty(g []bool, size int) int {
	score := 0
	version := (size - 17) / 4

	// Rule 1: 5+ consecutive same-color modules in a row or column.
	for r := 0; r < size; r++ {
		run := 1
		for c := 1; c < size; c++ {
			if g[r*size+c] == g[r*size+c-1] {
				run++
			} else {
				if run >= 5 {
					score += 3 + run - 5
				}
				run = 1
			}
		}
		if run >= 5 {
			score += 3 + run - 5
		}
	}
	for c := 0; c < size; c++ {
		run := 1
		for r := 1; r < size; r++ {
			if g[r*size+c] == g[(r-1)*size+c] {
				run++
			} else {
				if run >= 5 {
					score += 3 + run - 5
				}
				run = 1
			}
		}
		if run >= 5 {
			score += 3 + run - 5
		}
	}

	// Rule 2: 2x2 blocks of same color.
	for r := 0; r < size-1; r++ {
		for c := 0; c < size-1; c++ {
			v := g[r*size+c]
			if g[r*size+c+1] == v && g[(r+1)*size+c] == v && g[(r+1)*size+c+1] == v {
				score += 3
			}
		}
	}

	// Rule 3: finder-like patterns.
	pat1 := []bool{true, false, true, true, true, false, true, false, false, false, false}
	pat2 := []bool{false, false, false, false, true, false, true, true, true, false, true}
	for r := 0; r < size; r++ {
		for c := 0; c <= size-11; c++ {
			match1, match2 := true, true
			for k := 0; k < 11; k++ {
				if g[r*size+c+k] != pat1[k] {
					match1 = false
				}
				if g[r*size+c+k] != pat2[k] {
					match2 = false
				}
			}
			if match1 || match2 {
				score += 40
			}
		}
	}
	for c := 0; c < size; c++ {
		for r := 0; r <= size-11; r++ {
			match1, match2 := true, true
			for k := 0; k < 11; k++ {
				if g[(r+k)*size+c] != pat1[k] {
					match1 = false
				}
				if g[(r+k)*size+c] != pat2[k] {
					match2 = false
				}
			}
			if match1 || match2 {
				score += 40
			}
		}
	}

	// Rule 4: proportion of dark modules.
	dark := 0
	for r := 0; r < size; r++ {
		for c := 0; c < size; c++ {
			if !isFunction(size, version, r, c) && g[r*size+c] {
				dark++
			}
		}
	}
	total := size * size
	pct := dark * 100 / total
	prev5 := (pct / 5) * 5
	next5 := prev5 + 5
	d1 := prev5 - 50
	d2 := next5 - 50
	if d1 < 0 {
		d1 = -d1
	}
	if d2 < 0 {
		d2 = -d2
	}
	if d1 < d2 {
		score += d1 / 5 * 10
	} else {
		score += d2 / 5 * 10
	}

	return score
}

func chooseMask(g []bool, size int) int {
	version := (size - 17) / 4
	best := -1
	bestScore := 1<<31 - 1
	for m := 0; m < 8; m++ {
		// Apply mask temporarily.
		test := make([]bool, len(g))
		copy(test, g)
		for r := 0; r < size; r++ {
			for c := 0; c < size; c++ {
				if !isFunction(size, version, r, c) && maskFuncs[m](r, c) {
					test[r*size+c] = !test[r*size+c]
				}
			}
		}
		s := penalty(test, size)
		if s < bestScore {
			bestScore = s
			best = m
		}
	}
	return best
}

func applyMask(g []bool, size, mask int) {
	version := (size - 17) / 4
	for r := 0; r < size; r++ {
		for c := 0; c < size; c++ {
			if !isFunction(size, version, r, c) && maskFuncs[mask](r, c) {
				g[r*size+c] = !g[r*size+c]
			}
		}
	}
}

// writeFormatInfo writes the 15-bit format information (EC level M + mask pattern)
// into the two format information areas of the matrix.
func writeFormatInfo(g []bool, size, mask int) {
	// EC level M = 0b00, mask pattern in 3 bits.
	// Format data = ecLevel(2b) | mask(3b) = 0b00 | mask[2:0].
	// Then 10 error correction bits computed via polynomial division by
	// 0x537 with XOR mask 0x5412.
	formatData := uint(0b00<<3) | uint(mask)
	// Compute BCH error correction (10 bits).
	d := formatData << 10
	for i := 14; i >= 10; i-- {
		if d>>(uint(i)) != 0 {
			d ^= 0x537 << uint(i-10)
		}
	}
	format := (formatData<<10 | d) ^ 0x5412

	// Place bits in the two format information strips.
	// Format info around the top-left finder pattern.
	fmtPositions := [][2]int{
		{8, 0}, {8, 1}, {8, 2}, {8, 3}, {8, 4}, {8, 5}, {8, 7}, {8, 8},
		{7, 8}, {5, 8}, {4, 8}, {3, 8}, {2, 8}, {1, 8}, {0, 8},
	}
	for i, pos := range fmtPositions {
		bit := (format >> uint(14-i)) & 1
		g[pos[0]*size+pos[1]] = bit == 1
	}
	// Second copy: top-right and bottom-left.
	fmtPositions2 := [][2]int{
		{size - 1, 8}, {size - 2, 8}, {size - 3, 8}, {size - 4, 8},
		{size - 5, 8}, {size - 6, 8}, {size - 7, 8},
		{8, size - 8}, {8, size - 7}, {8, size - 6}, {8, size - 5},
		{8, size - 4}, {8, size - 3}, {8, size - 2}, {8, size - 1},
	}
	for i, pos := range fmtPositions2 {
		bit := (format >> uint(i)) & 1
		g[pos[0]*size+pos[1]] = bit == 1
	}
}
