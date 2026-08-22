package contextexec

import (
	"fmt"
	"io"
	"strings"

	core "github.com/maemreyo/shellbeam/internal/core/contextexec"
)

type CanonicalOutput struct {
	StdoutBytes int64
	StderrBytes int64
	Complete    bool
	Truncated   bool
}

var childControlEnvironment = map[string]struct{}{
	core.HelperRuntimeDirEnvironment:    {},
	"SHELLBEAM_CONTEXT_EXEC_SOCKET":     {},
	"SHELLBEAM_CONTEXT_EXEC_CLAIM":      {},
	"SHELLBEAM_CONTEXT_EXEC_GENERATION": {},
	"SHELLBEAM_CONTEXT_EXEC_LAUNCH_ID":  {},
	"SHELLBEAM_TRACE_SOCKET":            {},
	"SHELLBEAM_TRACE_PROTOCOL":          {},
	"SHELLBEAM_TRACE_ID":                {},
	"SHELLBEAM_PROVIDER_GENERATION":     {},
}

func SanitizeChildEnvironment(environment []string) []string {
	out := make([]string, 0, len(environment))
	for _, entry := range environment {
		key := entry
		if idx := strings.IndexByte(entry, '='); idx >= 0 {
			key = entry[:idx]
		}
		if _, control := childControlEnvironment[key]; control {
			continue
		}
		out = append(out, entry)
	}
	return out
}

type outputChunk struct {
	stream OutputStream
	data   []byte
	err    error
	done   bool
}

func CaptureOutput(stdout, stderr io.Reader, maxBytes int64, emit func(OutputFrame) error) (CanonicalOutput, error) {
	if stdout == nil || stderr == nil || maxBytes < 1 {
		return CanonicalOutput{}, fmt.Errorf("invalid context child output capture")
	}
	chunks := make(chan outputChunk, 8)
	read := func(stream OutputStream, r io.Reader) {
		defer func() { chunks <- outputChunk{stream: stream, done: true} }()
		buf := make([]byte, 16<<10)
		for {
			n, err := r.Read(buf)
			if n > 0 {
				chunks <- outputChunk{stream: stream, data: append([]byte(nil), buf[:n]...)}
			}
			if err != nil {
				if err != io.EOF {
					chunks <- outputChunk{stream: stream, err: err}
				}
				return
			}
		}
	}
	go read(StreamStdout, stdout)
	go read(StreamStderr, stderr)

	var result CanonicalOutput
	offsets := map[OutputStream]int64{StreamStdout: 0, StreamStderr: 0}
	remaining := maxBytes
	done := 0
	var firstErr error
	for done < 2 {
		chunk := <-chunks
		if chunk.done {
			done++
			continue
		}
		if chunk.err != nil {
			if firstErr == nil {
				firstErr = chunk.err
			}
			continue
		}
		data := chunk.data
		if int64(len(data)) > remaining {
			if remaining > 0 {
				data = data[:remaining]
			} else {
				data = nil
			}
			result.Truncated = true
		}
		if len(data) > 0 {
			frame := OutputFrame{ProtocolVersion: ProtocolVersion, Kind: KindOutput, Stream: chunk.stream, Offset: offsets[chunk.stream], Data: data}
			if emit != nil {
				if err := emit(frame); err != nil && firstErr == nil {
					firstErr = err
				}
			}
			offsets[chunk.stream] += int64(len(data))
			remaining -= int64(len(data))
			if chunk.stream == StreamStdout {
				result.StdoutBytes += int64(len(data))
			} else {
				result.StderrBytes += int64(len(data))
			}
		}
		if len(data) != len(chunk.data) {
			result.Truncated = true
		}
	}
	result.Complete = firstErr == nil && !result.Truncated
	return result, firstErr
}
