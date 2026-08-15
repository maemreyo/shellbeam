package environment

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	goVersionPattern     = regexp.MustCompile(`^go[0-9]+\.[0-9]+(?:\.[0-9]+)?(?:[a-z0-9.-]*)?$`)
	nodeVersionPattern   = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(?:[-+][0-9A-Za-z.-]+)?$`)
	pythonVersionPattern = regexp.MustCompile(`^Python ([0-9]+\.[0-9]+\.[0-9]+(?:[-+][0-9A-Za-z.-]+)?)$`)
	javaVersionPattern   = regexp.MustCompile(`^(?:openjdk|java) version "([^"]+)"(?: .*)?$`)
	rustVersionPattern   = regexp.MustCompile(`^rustc ([0-9]+\.[0-9]+\.[0-9]+(?:[-+][0-9A-Za-z.-]+)?) \([^\r\n]+\)$`)
)

func parseProbeVersion(kind string, output []byte) (string, error) {
	text := strings.TrimSpace(string(output))
	if text == "" {
		return "", fmt.Errorf("empty probe output")
	}
	switch kind {
	case "go":
		if strings.Contains(text, "\n") || !goVersionPattern.MatchString(text) {
			return "", fmt.Errorf("invalid go version output")
		}
		return text, nil
	case "node":
		if strings.Contains(text, "\n") || !nodeVersionPattern.MatchString(text) {
			return "", fmt.Errorf("invalid node version output")
		}
		return text, nil
	case "python":
		if strings.Contains(text, "\n") {
			return "", fmt.Errorf("invalid python version output")
		}
		match := pythonVersionPattern.FindStringSubmatch(text)
		if len(match) != 2 {
			return "", fmt.Errorf("invalid python version output")
		}
		return match[1], nil
	case "java":
		line := text
		if index := strings.IndexByte(line, '\n'); index >= 0 {
			line = strings.TrimSpace(line[:index])
		}
		match := javaVersionPattern.FindStringSubmatch(line)
		if len(match) != 2 {
			return "", fmt.Errorf("invalid java version output")
		}
		return match[1], nil
	case "rust":
		if strings.Contains(text, "\n") {
			return "", fmt.Errorf("invalid rust version output")
		}
		match := rustVersionPattern.FindStringSubmatch(text)
		if len(match) != 2 {
			return "", fmt.Errorf("invalid rust version output")
		}
		return match[1], nil
	default:
		return "", fmt.Errorf("unsupported toolchain")
	}
}
