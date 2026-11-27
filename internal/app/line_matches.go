package app

// Matching two lists of lines, each line by what it says: which line of the
// second each line of the first stays as. The lines both begin and end with
// alike match in place; between them the longest common sequence does, the
// one a table of every pair of lines picks when read from the start — a line
// both hold where both are, and otherwise the first's line taken away before
// the second's added, while the sequence stays longest. It is found from the
// lines that differ (Myers, "An O(ND) Difference Algorithm", 1986): memory
// grows as their number squared and time as it times the lines, not as the
// two counts multiplied, so a long page or report a person changed in a few
// places still matches line by line.

// lineMatchDifferences bounds the lines taken away and added between the
// lines two lists begin and end with alike (memory about 8 MB at the bound);
// past it those lines stay unmatched.
const lineMatchDifferences = 2048

// lineMatches is, for each of a's lines, the index of the line of b it stays
// as, or -1; false when more than lineMatchDifferences lines differ, and then
// only the lines a and b begin and end with alike match.
func lineMatches(a, b []string) ([]int, bool) {
	match := make([]int, len(a))
	for index := range match {
		match[index] = -1
	}
	start := 0
	for start < len(a) && start < len(b) && a[start] == b[start] {
		match[start] = start
		start++
	}
	endA, endB := len(a), len(b)
	for endA > start && endB > start && a[endA-1] == b[endB-1] {
		endA, endB = endA-1, endB-1
		match[endA] = endB
	}
	if endA == start || endB == start {
		return match, true
	}
	numbers := make(map[string]int32, endA-start+endB-start)
	number := func(lines []string) []int32 {
		out := make([]int32, len(lines))
		for index, line := range lines {
			id, seen := numbers[line]
			if !seen {
				id = int32(len(numbers))
				numbers[line] = id
			}
			out[index] = id
		}
		return out
	}
	x, y := number(a[start:endA]), number(b[start:endB])
	reach, differences, found := lineMatchReach(x, y)
	if !found {
		return match, false
	}
	// From the start, each step keeps the sequence longest: a line both hold
	// where both are, else x's line taken away when what is left then differs
	// in one line fewer, else y's line added.
	for i, j, left := 0, 0, differences; i < len(x) || j < len(y); {
		switch {
		case i < len(x) && j < len(y) && x[i] == y[j]:
			match[start+i] = start + j
			i, j = i+1, j+1
		case i < len(x) && (j == len(y) || reach.within(i+1, j, left-1)):
			i, left = i+1, left-1
		default:
			j, left = j+1, left-1
		}
	}
	return match, true
}

// lineMatchLayer is, for the diagonals i-j = lo, lo+2, … of one number of
// differences, the least i from which x[i:] and y[j:] differ in at most that
// many lines (len(x)+1 where none does).
type lineMatchLayer struct {
	lo int
	at []int32
}

// lineMatchLayers is lineMatchLayer for each number of differences from 0.
type lineMatchLayers struct {
	layers []lineMatchLayer
	end    int // len(x) - len(y)
}

// least is the layer of d differences on diagonal k, or unreached.
func (reach lineMatchLayers) least(d, k int, unreached int32) int32 {
	if d < 0 || d >= len(reach.layers) {
		return unreached
	}
	layer := reach.layers[d]
	if k < layer.lo || (k-layer.lo)&1 != 0 || (k-layer.lo)/2 >= len(layer.at) {
		return unreached
	}
	return layer.at[(k-layer.lo)/2]
}

// within tells whether x[i:] and y[j:] differ in at most d lines: the
// differences left always have the parity of the lines left.
func (reach lineMatchLayers) within(i, j, d int) bool {
	if (i-j-reach.end-d)&1 != 0 {
		d--
	}
	return reach.least(d, i-j, int32(i+1)) <= int32(i)
}

// lineMatchReach is, from the end back, how far each number of differences
// reaches on each diagonal, up to the number x and y differ in all, which is
// the second result; false past lineMatchDifferences. Along a diagonal the
// lines left differ no more the nearer the end, so a layer's least i holds
// every point after it.
func lineMatchReach(x, y []int32) (lineMatchLayers, int, bool) {
	n, m := len(x), len(y)
	reach := lineMatchLayers{end: n - m}
	unreached := int32(n + 1)
	// diagonalEnd is the greatest i on diagonal k.
	diagonalEnd := func(k int) int { return min(n, m+k) }
	value := func(d, k int) int32 { return reach.least(d, k, unreached) }
	for d := 0; d <= lineMatchDifferences; d++ {
		lo, hi := max(reach.end-d, -m), min(reach.end+d, n)
		if (lo-reach.end-d)&1 != 0 {
			lo++
		}
		layer := lineMatchLayer{lo: lo}
		if hi >= lo {
			layer.at = make([]int32, (hi-lo)/2+1)
		}
		for k := lo; k <= hi; k += 2 {
			best := value(d-2, k)
			if d == 0 {
				best = int32(n)
			}
			// Back from diagonal k+1 by taking x's line away again, or from
			// k-1 by taking y's line out again.
			if from := max(int(value(d-1, k+1)), 1); from <= diagonalEnd(k+1) {
				best = min(best, int32(from-1))
			}
			if from := max(int(value(d-1, k-1)), k); from <= diagonalEnd(k-1) {
				best = min(best, int32(from))
			}
			if best != unreached {
				i := int(best)
				for i > 0 && i-k > 0 && x[i-1] == y[i-k-1] {
					i--
				}
				best = int32(i)
			}
			layer.at[(k-lo)/2] = best
		}
		reach.layers = append(reach.layers, layer)
		if lo <= 0 && 0 <= hi && (0-lo)&1 == 0 && layer.at[(0-lo)/2] == 0 {
			return reach, d, true
		}
	}
	return lineMatchLayers{}, 0, false
}
