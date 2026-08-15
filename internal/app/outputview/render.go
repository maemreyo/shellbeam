package outputview

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

var ansiCSI = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)

func (s *Service) readPreview(ctx context.Context, out Result, sel Selector) (Result, error) {
	cut := out.FrozenCutBytes
	budget := int64(sel.HeadBytes + sel.TailBytes)
	if cut <= budget {
		data, err := s.readExactWindow(ctx, out.SessionID, 0, int(cut))
		if err != nil {
			return Result{}, err
		}
		out.Text = renderedText(data)
		out.Ranges = []RawRange{{Start: 0, End: cut}}
		return out, nil
	}
	head, err := s.readExactWindow(ctx, out.SessionID, 0, sel.HeadBytes)
	if err != nil {
		return Result{}, err
	}
	tailStart := cut - int64(sel.TailBytes)
	tail, err := s.readExactWindow(ctx, out.SessionID, tailStart, sel.TailBytes)
	if err != nil {
		return Result{}, err
	}
	head, headEnd := trimIncompleteSuffix(head, int64(len(head)))
	tail, tailStart = trimIncompletePrefix(tail, tailStart)
	omitted := tailStart - headEnd
	out.Text = renderedText(head) + fmt.Sprintf("\n… %d raw bytes omitted …\n", omitted) + renderedText(tail)
	out.Ranges = []RawRange{{Start: 0, End: headEnd}, {Start: tailStart, End: cut}}
	out.Truncated = true
	return out, nil
}

func renderedText(data []byte) string {
	text := strings.ToValidUTF8(string(data), "�")
	text = ansiCSI.ReplaceAllString(text, "")
	if binaryLike([]byte(text)) {
		return fmt.Sprintf("[binary output: %d bytes]", len(data))
	}
	return collapseCarriageReturns(text)
}

func collapseCarriageReturns(text string) string {
	parts := strings.SplitAfter(text, "\n")
	var b strings.Builder
	for _, part := range parts {
		suffix := ""
		body := part
		if strings.HasSuffix(body, "\n") {
			body, suffix = strings.TrimSuffix(body, "\n"), "\n"
		}
		if i := strings.LastIndexByte(body, '\r'); i >= 0 {
			body = body[i+1:]
		}
		b.WriteString(body)
		b.WriteString(suffix)
	}
	return b.String()
}

func binaryLike(data []byte) bool {
	for _, b := range data {
		if b == '\n' || b == '\r' || b == '\t' {
			continue
		}
		if b < 0x20 || b == 0x7f {
			return true
		}
	}
	return false
}

func trimIncompleteSuffix(data []byte, end int64) ([]byte, int64) {
	for len(data) > 0 && !utf8.Valid(data) {
		_, size := utf8.DecodeLastRune(data)
		if size > 1 || data[len(data)-1] < utf8.RuneSelf {
			break
		}
		data = data[:len(data)-1]
		end--
	}
	return data, end
}

func trimIncompletePrefix(data []byte, start int64) ([]byte, int64) {
	for len(data) > 0 && data[0]&0xc0 == 0x80 {
		data = data[1:]
		start++
	}
	return data, start
}
