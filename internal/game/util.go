package game

import (
	"crypto/rand"
	"encoding/hex"
	"strconv"
	"strings"
)

func itoa(n int) string { return strconv.Itoa(n) }

func joinComma(parts []string) string { return strings.Join(parts, ", ") }

func newID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// strconvFloat renders a float without trailing zeros, used by fmtMoney.
func strconvFloat(v float64) string {
	return strconv.FormatFloat(v, 'f', 0, 64)
}
