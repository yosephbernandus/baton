package ansi

import "regexp"

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

func Strip(s string) string {
	return ansiRe.ReplaceAllString(s, "")
}

func StripLines(lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = Strip(l)
	}
	return out
}
