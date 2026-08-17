package media

import (
	"errors"
	"strings"
	"unicode/utf8"
)

var (
	errInvalidLogicalPath    = errors.New("invalid logical media path")
	errInvalidCWD            = errors.New("invalid media cwd")
	errInvalidDisplayAddress = errors.New("invalid media display address")
)

type LogicalPath struct {
	Raw        string
	Components []string
}

func ParseLogicalPath(raw string) (LogicalPath, error) {
	if raw == "" || len(raw) > MaxPathBytes || !utf8.ValidString(raw) || strings.HasPrefix(raw, "/") || strings.ContainsRune(raw, '\x00') || strings.Contains(raw, `\`) || strings.HasSuffix(raw, "/") {
		return LogicalPath{}, errInvalidLogicalPath
	}
	parts := strings.Split(raw, "/")
	if len(parts) == 0 || len(parts) > MaxPathComponents {
		return LogicalPath{}, errInvalidLogicalPath
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return LogicalPath{}, errInvalidLogicalPath
		}
	}
	return LogicalPath{Raw: raw, Components: append([]string(nil), parts...)}, nil
}

func ValidateCWD(raw string) error {
	if raw == "" || len(raw) > MaxCWDBytes || !utf8.ValidString(raw) || !strings.HasPrefix(raw, "/") || strings.ContainsRune(raw, '\x00') {
		return errInvalidCWD
	}
	return nil
}
