package dyld

import (
	"encoding/binary"
	"fmt"

	trace "github.com/maemreyo/shellbeam/internal/core/inputtrace"
)

const (
	wireProtocolVersion = 1
	wireHeaderBytes     = 12
	maxRawPathBytes     = 4096
	maxWireEventBytes   = wireHeaderBytes + maxRawPathBytes
)

type eventClass uint8

const (
	eventFilesystemRead eventClass = iota + 1
	eventMetadataQuery
	eventDirectoryEnumeration
	eventFilesystemWrite
	eventExecutedBinary
	eventLoadedLibrary
)

type wireEvent struct {
	class eventClass
	flags uint16
	pid   uint32
	path  string
}

func encodeEvent(class eventClass, pid uint32, path string) []byte {
	raw := make([]byte, wireHeaderBytes+len(path))
	raw[0] = wireProtocolVersion
	raw[1] = byte(class)
	binary.LittleEndian.PutUint16(raw[2:4], 0)
	binary.LittleEndian.PutUint32(raw[4:8], pid)
	binary.LittleEndian.PutUint32(raw[8:12], uint32(len(path)))
	copy(raw[wireHeaderBytes:], path)
	return raw
}

func decodeEvent(raw []byte) (wireEvent, error) {
	if len(raw) < wireHeaderBytes || len(raw) > maxWireEventBytes || raw[0] != wireProtocolVersion {
		return wireEvent{}, fmt.Errorf("invalid input trace datagram")
	}
	class := eventClass(raw[1])
	if _, ok := observationClass(class); !ok {
		return wireEvent{}, fmt.Errorf("unknown input trace event class")
	}
	length := int(binary.LittleEndian.Uint32(raw[8:12]))
	if length < 1 || length > maxRawPathBytes || wireHeaderBytes+length != len(raw) {
		return wireEvent{}, fmt.Errorf("invalid input trace path length")
	}
	path := string(raw[wireHeaderBytes:])
	if len(path) == 0 {
		return wireEvent{}, fmt.Errorf("empty input trace path")
	}
	return wireEvent{class: class, flags: binary.LittleEndian.Uint16(raw[2:4]), pid: binary.LittleEndian.Uint32(raw[4:8]), path: path}, nil
}

func observationClass(class eventClass) (trace.ObservationClass, bool) {
	switch class {
	case eventFilesystemRead:
		return trace.ClassFilesystemReads, true
	case eventMetadataQuery:
		return trace.ClassFilesystemMetadataQueries, true
	case eventDirectoryEnumeration:
		return trace.ClassDirectoryEnumerations, true
	case eventFilesystemWrite:
		return trace.ClassFilesystemWrites, true
	case eventExecutedBinary:
		return trace.ClassExecutedBinaries, true
	case eventLoadedLibrary:
		return trace.ClassLoadedLibraries, true
	default:
		return "", false
	}
}

func providerCoverage() trace.CoverageMatrix {
	return trace.CoverageMatrix{
		FilesystemReads:           trace.CoveragePartial,
		FilesystemMetadataQueries: trace.CoveragePartial,
		DirectoryEnumerations:     trace.CoveragePartial,
		FilesystemWrites:          trace.CoveragePartial,
		ExecutedBinaries:          trace.CoveragePartial,
		LoadedLibraries:           trace.CoveragePartial,
		EnvironmentNamesObserved:  trace.CoverageUnsupported,
		NetworkAttempts:           trace.CoverageUnsupported,
		ChildProcesses:            trace.CoveragePartial,
	}
}
