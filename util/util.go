package util

import "strings"

func CutAny[T ~string | ~[]byte](b T, chars T) (before, after T, ok bool) {
	s := string(b)
	i := strings.IndexAny(s, string(chars))
	if i < 0 {
		return b, after, false
	}
	return T(s[:i]), T(strings.TrimLeft(s, string(chars))), true
}
