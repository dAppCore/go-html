package php

// This file collects small package-local helpers that fill gaps in the
// pinned dappco.re/go release (currently v0.9.0). Each helper has a
// matching core/go primitive landing post-#1329; remove this file and
// inline the canonical core wrapper once the dappco.re/go bump lands.

// trimTrailingSlash strips trailing "/" runes from s. Equivalent of
// strings.TrimRight(s, "/") for single-rune trim without importing
// strings; replaced by core.TrimRight when available.
func trimTrailingSlash(s string) string {
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}

// repeat returns a string consisting of n copies of s. Equivalent of
// strings.Repeat without importing strings; replaced by core.Repeat
// when available.
func repeat(s string, n int) string {
	if n <= 0 {
		return ""
	}
	out := make([]byte, 0, len(s)*n)
	for range n {
		out = append(out, s...)
	}
	return string(out)
}

// stringBuilder accumulates strings and joins them at the end.
// Equivalent to strings.Builder for write-heavy use without importing
// strings; replaced by core.Builder when available.
type stringBuilder []string

func (b *stringBuilder) WriteString(s string) {
	*b = append(*b, s)
}

func (b *stringBuilder) String() string {
	out := ""
	for _, s := range *b {
		out += s
	}
	return out
}
