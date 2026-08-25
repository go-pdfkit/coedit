package coedit

import (
	"strconv"
	"strings"
)

// A crop box travels as four numbers with a comma between them, which is short,
// exact, and the same on every architecture — a page's geometry has to mean the
// same thing in a browser as on a server.
const boxSeparator = ","

// encodeBox writes a rectangle.
func encodeBox(box []float64) string {
	parts := make([]string, len(box))
	for i, v := range box {
		parts[i] = strconv.FormatFloat(v, 'g', -1, 64)
	}
	return strings.Join(parts, boxSeparator)
}

// decodeBox reads one back, or nothing for anything that is not one.
func decodeBox(s string) []float64 {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, boxSeparator)
	if len(parts) != 4 {
		return nil
	}
	out := make([]float64, 4)
	for i, part := range parts {
		v, err := strconv.ParseFloat(part, 64)
		if err != nil {
			return nil
		}
		out[i] = v
	}
	return out
}
