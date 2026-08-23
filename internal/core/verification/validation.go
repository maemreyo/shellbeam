package verification

import (
	"encoding/hex"
	"fmt"
	"strings"
	"unicode/utf8"
)

func boundedToken(v string, max int) bool {
	return v != "" && len(v) <= max && utf8.ValidString(v) && !strings.ContainsAny(v, "\x00\r\n")
}
func validateStrings(v []string, maxItems, maxBytes int) error {
	if len(v) > maxItems {
		return fmt.Errorf("too many values")
	}
	for _, s := range v {
		if !boundedToken(s, maxBytes) {
			return fmt.Errorf("invalid bounded value")
		}
	}
	return nil
}
func validateRefs(v []string, maxItems, maxBytes int) error {
	return validateStrings(v, maxItems, maxBytes)
}
func validGeneration(v string) bool {
	if !strings.HasPrefix(v, "gen_") || len(v) != 68 {
		return false
	}
	_, err := hex.DecodeString(v[4:])
	return err == nil
}
