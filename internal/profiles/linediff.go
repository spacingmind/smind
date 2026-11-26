package profiles

import "strings"

// LineDiff is a unified-style line diff of a against b (a classic LCS over
// lines): " " keeps a line, "-" removes one of a, "+" adds one of b.
func LineDiff(a, b string) []string {
	x, y := strings.Split(a, "\n"), strings.Split(b, "\n")
	lcs := make([][]int, len(x)+1)
	for i := range lcs {
		lcs[i] = make([]int, len(y)+1)
	}
	for i := len(x) - 1; i >= 0; i-- {
		for j := len(y) - 1; j >= 0; j-- {
			switch {
			case x[i] == y[j]:
				lcs[i][j] = lcs[i+1][j+1] + 1
			case lcs[i+1][j] >= lcs[i][j+1]:
				lcs[i][j] = lcs[i+1][j]
			default:
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}
	var out []string
	i, j := 0, 0
	for i < len(x) && j < len(y) {
		switch {
		case x[i] == y[j]:
			out = append(out, " "+x[i])
			i++
			j++
		case lcs[i+1][j] >= lcs[i][j+1]:
			out = append(out, "-"+x[i])
			i++
		default:
			out = append(out, "+"+y[j])
			j++
		}
	}
	for ; i < len(x); i++ {
		out = append(out, "-"+x[i])
	}
	for ; j < len(y); j++ {
		out = append(out, "+"+y[j])
	}
	return out
}

// UnifiedDiff is LineDiff joined into one text.
func UnifiedDiff(a, b string) string {
	return strings.Join(LineDiff(a, b), "\n")
}
