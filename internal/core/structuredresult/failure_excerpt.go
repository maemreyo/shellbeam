package structuredresult

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/maemreyo/shellbeam/internal/core/inputtrace"
)

const MaxFailureExcerptBytes = 2048

type FailureExcerpt struct {
	Namespace         string `json:"namespace"`
	VocabularyVersion int    `json:"vocabulary_version"`
	Text              string `json:"text"`
	Truncated         bool   `json:"truncated"`
	Redacted          bool   `json:"redacted"`
}

func (e FailureExcerpt) Validate() error {
	if !safeStructuredText(e.Namespace, 64) || e.VocabularyVersion != 1 || !utf8.ValidString(e.Text) || len(e.Text) > MaxFailureExcerptBytes || strings.TrimSpace(e.Text) == "" {
		return fmt.Errorf("invalid failure excerpt")
	}
	for _, r := range e.Text {
		if r != '\n' && unicode.IsControl(r) {
			return fmt.Errorf("invalid failure excerpt control")
		}
	}
	if failureExcerptContainsAbsolutePath(e.Text) {
		return fmt.Errorf("failure excerpt contains absolute path")
	}
	return nil
}

func failureExcerptContainsAbsolutePath(text string) bool {
	for i := 0; i < len(text); i++ {
		if text[i] == '/' && failureExcerptPathBoundary(text, i) {
			return true
		}
	}
	return false
}

func NormalizeFailureExcerpt(raw, namespace, workspaceRoot string) (FailureExcerpt, bool) {
	if !utf8.ValidString(raw) || !safeStructuredText(namespace, 64) {
		return FailureExcerpt{}, false
	}
	text := stripFailureExcerptEscapes(raw)
	text = stripFailureExcerptControls(text)
	var ok bool
	var redacted bool
	text, redacted, ok = normalizeFailureExcerptPaths(text, workspaceRoot)
	if !ok || strings.TrimSpace(text) == "" {
		return FailureExcerpt{}, false
	}
	text, truncated := truncateFailureExcerpt(text)
	if strings.TrimSpace(text) == "" {
		return FailureExcerpt{}, false
	}
	return FailureExcerpt{
		Namespace:         namespace,
		VocabularyVersion: 1,
		Text:              text,
		Truncated:         truncated,
		Redacted:          redacted,
	}, true
}

func stripFailureExcerptEscapes(raw string) string {
	var out strings.Builder
	out.Grow(len(raw))
	for i := 0; i < len(raw); {
		if raw[i] != 0x1b {
			out.WriteByte(raw[i])
			i++
			continue
		}
		if i+1 >= len(raw) {
			break
		}
		switch raw[i+1] {
		case '[':
			j := i + 2
			for ; j < len(raw); j++ {
				if raw[j] >= 0x40 && raw[j] <= 0x7e {
					j++
					break
				}
			}
			if j > len(raw) || j == len(raw) && (len(raw) == 0 || raw[len(raw)-1] < 0x40 || raw[len(raw)-1] > 0x7e) {
				return out.String()
			}
			i = j
		case ']':
			j := i + 2
			terminated := false
			for j < len(raw) {
				if raw[j] == 0x07 {
					j++
					terminated = true
					break
				}
				if raw[j] == 0x1b && j+1 < len(raw) && raw[j+1] == '\\' {
					j += 2
					terminated = true
					break
				}
				j++
			}
			if !terminated {
				return out.String()
			}
			i = j
		default:
			// Unknown ESC introducer: drop ESC itself and preserve following text.
			i++
		}
	}
	return out.String()
}

func stripFailureExcerptControls(text string) string {
	var out strings.Builder
	out.Grow(len(text))
	for _, r := range text {
		if r != '\n' && unicode.IsControl(r) {
			continue
		}
		out.WriteRune(r)
	}
	return out.String()
}

func normalizeFailureExcerptPaths(text, workspaceRoot string) (string, bool, bool) {
	root := filepath.Clean(workspaceRoot)
	rootValid := filepath.IsAbs(root)
	redacted := false
	var out strings.Builder
	out.Grow(len(text))
	for i := 0; i < len(text); {
		if text[i] != '/' || !failureExcerptPathBoundary(text, i) {
			out.WriteByte(text[i])
			i++
			continue
		}
		end := i + 1
		for end < len(text) && !failureExcerptPathTerminator(text[end]) {
			end++
		}
		token := text[i:end]
		replacement, class, ok := classifyFailureExcerptAbsolutePath(token, root, rootValid)
		if !ok {
			return "", false, false
		}
		if class == inputtrace.PathWorkspaceExternalRedacted {
			redacted = true
		}
		out.WriteString(replacement)
		i = end
	}
	return out.String(), redacted, true
}

func failureExcerptPathBoundary(text string, i int) bool {
	if i == 0 {
		return true
	}
	r, _ := utf8.DecodeLastRuneInString(text[:i])
	if unicode.IsSpace(r) {
		return true
	}
	return strings.ContainsRune("([{<'\"=:,", r)
}

func failureExcerptPathTerminator(b byte) bool {
	if b < utf8.RuneSelf {
		if unicode.IsSpace(rune(b)) {
			return true
		}
		return strings.ContainsRune(")]}>\"',;", rune(b))
	}
	return false
}

func classifyFailureExcerptAbsolutePath(token, root string, rootValid bool) (string, inputtrace.PathClass, bool) {
	clean := filepath.Clean(token)
	if !filepath.IsAbs(clean) {
		return "", "", false
	}
	if rootValid {
		if rel, err := filepath.Rel(root, clean); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return filepath.ToSlash(rel), inputtrace.PathRepoRelative, true
		}
	}
	if class := failureExcerptSystemClass(clean); class != "" {
		return "[" + string(inputtrace.PathSystemClassified) + ":" + class + "]", inputtrace.PathSystemClassified, true
	}
	if !rootValid {
		return "", "", false
	}
	return "[" + string(inputtrace.PathWorkspaceExternalRedacted) + "]", inputtrace.PathWorkspaceExternalRedacted, true
}

func failureExcerptSystemClass(path string) string {
	for _, item := range []struct {
		prefix string
		class  string
	}{
		{prefix: "/usr", class: "usr"},
		{prefix: "/System", class: "system"},
		{prefix: "/Library", class: "library"},
		{prefix: "/bin", class: "system"},
		{prefix: "/sbin", class: "system"},
		{prefix: "/dev", class: "device"},
	} {
		if path == item.prefix || strings.HasPrefix(path, item.prefix+string(filepath.Separator)) {
			return item.class
		}
	}
	return ""
}

func truncateFailureExcerpt(text string) (string, bool) {
	if len(text) <= MaxFailureExcerptBytes {
		return text, false
	}
	end := MaxFailureExcerptBytes
	for end > 0 && !utf8.ValidString(text[:end]) {
		end--
	}
	return text[:end], true
}
