package main

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

const maxControlLineBytes = 64 << 10

type EventKind string

const (
	EventCommandEnd          EventKind = "command_end"
	EventCommandError        EventKind = "command_error"
	EventPaneOutput          EventKind = "pane_output"
	EventMessage             EventKind = "message"
	EventClientDetached      EventKind = "client_detached"
	EventExit                EventKind = "exit"
	EventUnknownNotification EventKind = "unknown_notification"
)

type ControlEvent struct {
	Kind          EventKind `json:"kind"`
	CommandNumber int       `json:"command_number,omitempty"`
	PaneID        string    `json:"pane_id,omitempty"`
	Data          string    `json:"data,omitempty"`
	Raw           string    `json:"raw,omitempty"`
}

type commandBlock struct {
	number int
	lines  []string
}

func parseControl(r io.Reader) ([]ControlEvent, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 1024), maxControlLineBytes)
	var events []ControlEvent
	var block *commandBlock
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if len(line) >= maxControlLineBytes {
			return nil, fmt.Errorf("control line too long at line %d", lineNo)
		}
		if block != nil {
			if strings.HasPrefix(line, "%begin ") {
				return nil, fmt.Errorf("nested %%begin at line %d", lineNo)
			}
			if strings.HasPrefix(line, "%end ") || strings.HasPrefix(line, "%error ") {
				number, err := parseBlockTerminator(line)
				if err != nil {
					return nil, fmt.Errorf("line %d: %w", lineNo, err)
				}
				if number != block.number {
					return nil, fmt.Errorf("command number mismatch at line %d: begin=%d end=%d", lineNo, block.number, number)
				}
				kind := EventCommandEnd
				if strings.HasPrefix(line, "%error ") {
					kind = EventCommandError
				}
				events = append(events, ControlEvent{Kind: kind, CommandNumber: block.number, Data: strings.Join(block.lines, "\n")})
				block = nil
				continue
			}
			if strings.HasPrefix(line, "%") {
				return nil, fmt.Errorf("notification inside command block at line %d: %s", lineNo, line)
			}
			block.lines = append(block.lines, line)
			continue
		}

		if strings.HasPrefix(line, "%begin ") {
			number, err := parseBegin(line)
			if err != nil {
				return nil, fmt.Errorf("line %d: %w", lineNo, err)
			}
			block = &commandBlock{number: number}
			continue
		}
		if !strings.HasPrefix(line, "%") {
			return nil, fmt.Errorf("unexpected control text outside command block at line %d", lineNo)
		}
		event, err := parseNotification(line)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNo, err)
		}
		events = append(events, event)
	}
	if err := scanner.Err(); err != nil {
		if err == bufio.ErrTooLong {
			return nil, fmt.Errorf("control line too long: %w", err)
		}
		return nil, err
	}
	if block != nil {
		return nil, fmt.Errorf("EOF mid-block for command %d", block.number)
	}
	return events, nil
}

func parseBegin(line string) (int, error) {
	fields := strings.Fields(line)
	if len(fields) != 4 || fields[0] != "%begin" {
		return 0, fmt.Errorf("malformed %%begin %q", line)
	}
	if _, err := strconv.ParseInt(fields[1], 10, 64); err != nil {
		return 0, fmt.Errorf("malformed %%begin timestamp: %w", err)
	}
	n, err := strconv.Atoi(fields[2])
	if err != nil || n < 0 {
		return 0, fmt.Errorf("malformed %%begin command number %q", fields[2])
	}
	if _, err := strconv.ParseUint(fields[3], 10, 64); err != nil {
		return 0, fmt.Errorf("malformed %%begin flags: %w", err)
	}
	return n, nil
}

func parseBlockTerminator(line string) (int, error) {
	fields := strings.Fields(line)
	if len(fields) != 4 || (fields[0] != "%end" && fields[0] != "%error") {
		return 0, fmt.Errorf("malformed block terminator %q", line)
	}
	if _, err := strconv.ParseInt(fields[1], 10, 64); err != nil {
		return 0, fmt.Errorf("malformed block timestamp: %w", err)
	}
	n, err := strconv.Atoi(fields[2])
	if err != nil || n < 0 {
		return 0, fmt.Errorf("malformed command number %q", fields[2])
	}
	if _, err := strconv.ParseUint(fields[3], 10, 64); err != nil {
		return 0, fmt.Errorf("malformed block flags: %w", err)
	}
	return n, nil
}

func parseNotification(line string) (ControlEvent, error) {
	switch {
	case strings.HasPrefix(line, "%output "):
		rest := strings.TrimPrefix(line, "%output ")
		space := strings.IndexByte(rest, ' ')
		if space <= 0 {
			return ControlEvent{}, fmt.Errorf("malformed %%output %q", line)
		}
		paneID := rest[:space]
		if len(paneID) < 2 || paneID[0] != '%' {
			return ControlEvent{}, fmt.Errorf("malformed pane id %q", paneID)
		}
		decoded, err := decodeControlOutput(rest[space+1:])
		if err != nil {
			return ControlEvent{}, err
		}
		return ControlEvent{Kind: EventPaneOutput, PaneID: paneID, Data: decoded}, nil
	case strings.HasPrefix(line, "%message"):
		return ControlEvent{Kind: EventMessage, Data: notificationData(line, "%message")}, nil
	case strings.HasPrefix(line, "%client-detached"):
		return ControlEvent{Kind: EventClientDetached, Data: notificationData(line, "%client-detached")}, nil
	case strings.HasPrefix(line, "%exit"):
		return ControlEvent{Kind: EventExit, Data: notificationData(line, "%exit")}, nil
	case strings.HasPrefix(line, "%end ") || strings.HasPrefix(line, "%error "):
		return ControlEvent{}, fmt.Errorf("block terminator without %%begin: %s", line)
	default:
		return ControlEvent{Kind: EventUnknownNotification, Raw: line}, nil
	}
}

func notificationData(line, prefix string) string {
	return strings.TrimPrefix(strings.TrimPrefix(line, prefix), " ")
}

func decodeControlOutput(s string) (string, error) {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		if s[i] != '\\' {
			b.WriteByte(s[i])
			i++
			continue
		}
		if i+3 >= len(s) || !isOctal(s[i+1]) || !isOctal(s[i+2]) || !isOctal(s[i+3]) {
			return "", fmt.Errorf("invalid control output escape at byte %d", i)
		}
		v := int(s[i+1]-'0')*64 + int(s[i+2]-'0')*8 + int(s[i+3]-'0')
		if v > 255 {
			return "", fmt.Errorf("invalid control output escape at byte %d", i)
		}
		b.WriteByte(byte(v))
		i += 4
	}
	return b.String(), nil
}

func isOctal(b byte) bool { return b >= '0' && b <= '7' }
