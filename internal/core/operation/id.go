// Package operation defines immutable execution intent and idempotency identity.
package operation

import (
	"fmt"
	"regexp"
)

type ID string
type SessionID string

var idPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)

func ParseID(v string) (ID, error) {
	if !idPattern.MatchString(v) {
		return "", fmt.Errorf("invalid operation id")
	}
	return ID(v), nil
}

func ParseSessionID(v string) (SessionID, error) {
	if !idPattern.MatchString(v) {
		return "", fmt.Errorf("invalid session id")
	}
	return SessionID(v), nil
}
