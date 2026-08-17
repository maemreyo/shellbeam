package mcp

import "strings"

func validReproIDInput(value string) bool {
	if len(value) != 32 || !strings.HasPrefix(value, "repro_") {
		return false
	}
	for _, r := range value[6:] {
		if strings.ContainsRune("0123456789ABCDEFGHJKMNPQRSTVWXYZ", r) {
			continue
		}
		return false
	}
	return true
}
