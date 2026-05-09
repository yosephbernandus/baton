package phase

type LoopDetector struct {
	history   [][]string
	window    int
	threshold float64
	tailLines int
}

func NewLoopDetector(window int, threshold float64, tailLines int) *LoopDetector {
	if window < 2 {
		window = 3
	}
	if threshold <= 0 {
		threshold = 0.9
	}
	if tailLines <= 0 {
		tailLines = 50
	}
	return &LoopDetector{
		window:    window,
		threshold: threshold,
		tailLines: tailLines,
	}
}

func (d *LoopDetector) Record(output []string) {
	tail := output
	if len(tail) > d.tailLines {
		tail = tail[len(tail)-d.tailLines:]
	}
	cp := make([]string, len(tail))
	copy(cp, tail)
	d.history = append(d.history, cp)
	if len(d.history) > d.window {
		d.history = d.history[len(d.history)-d.window:]
	}
}

func (d *LoopDetector) IsStuck() bool {
	if len(d.history) < d.window {
		return false
	}

	// All pairwise comparisons in the window must exceed threshold
	entries := d.history[len(d.history)-d.window:]
	for i := 0; i < len(entries); i++ {
		for j := i + 1; j < len(entries); j++ {
			if Similarity(entries[i], entries[j]) < d.threshold {
				return false
			}
		}
	}
	return true
}

// Similarity computes Jaccard similarity between two line sets.
// Returns (intersection size) / (union size).
func Similarity(a, b []string) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 1.0
	}
	if len(a) == 0 || len(b) == 0 {
		return 0.0
	}

	setA := make(map[string]struct{}, len(a))
	for _, line := range a {
		setA[line] = struct{}{}
	}

	setB := make(map[string]struct{}, len(b))
	for _, line := range b {
		setB[line] = struct{}{}
	}

	intersection := 0
	for line := range setA {
		if _, ok := setB[line]; ok {
			intersection++
		}
	}

	// Union = |A| + |B| - |intersection| (using unique counts)
	union := len(setA) + len(setB) - intersection
	if union == 0 {
		return 1.0
	}

	return float64(intersection) / float64(union)
}
