package structuredresult

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	core "github.com/maemreyo/shellbeam/internal/core/structuredresult"
)

type rawReaderProbe struct {
	data          []byte
	readCalls     int
	describeCalls int
}

func (r *rawReaderProbe) ReadInputRange(_ context.Context, _ core.StructuredInputRef, offset int64, max int) ([]byte, error) {
	r.readCalls++
	end := min(int(offset)+max, len(r.data))
	if int(offset) > len(r.data) {
		return nil, errors.New("raw range")
	}
	return append([]byte(nil), r.data[offset:end]...), nil
}
func (r *rawReaderProbe) DescribeInput(context.Context, core.StructuredInputRef) (InputContext, error) {
	r.describeCalls++
	return InputContext{OperationID: "raw-op", RepositoryRoot: "/repo"}, nil
}

type artifactInputStoreProbe struct {
	data    []byte
	context InputContext
	err     error
	calls   int
	ref     core.ArtifactBlobRef
	offset  int64
	max     int
}

func (s *artifactInputStoreProbe) DescribeArtifactInput(_ context.Context, ref core.ArtifactBlobRef) (InputContext, error) {
	if s.err != nil {
		return InputContext{}, s.err
	}
	if s.context.OperationID == "" {
		return InputContext{OperationID: ref.OperationID}, nil
	}
	return s.context, nil
}

func (s *artifactInputStoreProbe) ReadArtifactBlobRange(_ context.Context, ref core.ArtifactBlobRef, offset int64, max int) ([]byte, error) {
	s.calls++
	s.ref, s.offset, s.max = ref, offset, max
	if s.err != nil {
		return nil, s.err
	}
	end := min(int(offset)+max, len(s.data))
	if int(offset) > len(s.data) {
		return nil, errors.New("artifact range")
	}
	return append([]byte(nil), s.data[offset:end]...), nil
}

func readerArtifactRef(t *testing.T) core.ArtifactBlobRef {
	t.Helper()
	return core.ArtifactBlobRef{
		SchemaVersion: core.ArtifactBlobSchemaVersion,
		BlobID:        "abl_" + strings.Repeat("a", 64), OperationID: "artifact-reader-op", SessionID: "artifact-reader-session",
		RepositoryID: "repo_01M09A27JCSE71BXSP477EKN34", WorkspaceID: "ws_01M0CJB0KMBXWM7C7YDFYHBT2Q",
		DeclaredPath: "reports/junit.xml", NormalizedWorkspacePath: "reports/junit.xml",
		SHA256: strings.Repeat("b", 64), Size: 8,
		TerminalCut:    core.TerminalCutV1{SchemaVersion: core.TerminalCutSchemaVersion, ReceiptSchemaVersion: 1, ReceiptDigest: strings.Repeat("c", 64)},
		ObservationCut: core.ObservationCutV1{SchemaVersion: core.ObservationCutSchemaVersion, Digest: strings.Repeat("d", 64)},
	}
}

func TestArtifactReaderLeavesRawInputByteForByteUnchanged(t *testing.T) {
	raw := &rawReaderProbe{data: []byte("abcdef")}
	artifacts := &artifactInputStoreProbe{data: []byte("artifact")}
	reader, err := NewArtifactReader(raw, artifacts)
	if err != nil {
		t.Fatal(err)
	}
	ref := core.RawInputRef(core.RawOutputRef{SessionID: "raw-session", StartByte: 0, EndByte: 6, SHA256: strings.Repeat("e", 64)})
	got, err := reader.ReadInputRange(context.Background(), ref, 2, 3)
	if err != nil || string(got) != "cde" {
		t.Fatalf("raw=%q err=%v", got, err)
	}
	ctx, err := reader.DescribeInput(context.Background(), ref)
	if err != nil || ctx.OperationID != "raw-op" || ctx.RepositoryRoot != "/repo" {
		t.Fatalf("ctx=%#v err=%v", ctx, err)
	}
	if raw.readCalls != 1 || raw.describeCalls != 1 || artifacts.calls != 0 {
		t.Fatalf("raw=%d/%d artifact=%d", raw.readCalls, raw.describeCalls, artifacts.calls)
	}
}

func TestArtifactReaderUsesOnlyPrivateArtifactResolverAndReturnsArtifactOperationContext(t *testing.T) {
	raw := &rawReaderProbe{data: []byte("raw")}
	artifacts := &artifactInputStoreProbe{data: []byte("01234567"), context: InputContext{OperationID: "artifact-reader-op", RepositoryRoot: "/repo"}}
	reader, err := NewArtifactReader(raw, artifacts)
	if err != nil {
		t.Fatal(err)
	}
	ref := readerArtifactRef(t)
	input := core.ArtifactInputRef(ref)
	got, err := reader.ReadInputRange(context.Background(), input, 3, 3)
	if err != nil || string(got) != "345" {
		t.Fatalf("artifact=%q err=%v", got, err)
	}
	if artifacts.calls != 1 || !reflect.DeepEqual(artifacts.ref, ref) || artifacts.offset != 3 || artifacts.max != 3 {
		t.Fatalf("artifact probe=%#v", artifacts)
	}
	ctx, err := reader.DescribeInput(context.Background(), input)
	if err != nil || ctx.OperationID != ref.OperationID || ctx.RepositoryRoot != "/repo" {
		t.Fatalf("ctx=%#v err=%v", ctx, err)
	}
	if raw.readCalls != 0 || raw.describeCalls != 0 {
		t.Fatalf("artifact path delegated to raw reader: %d/%d", raw.readCalls, raw.describeCalls)
	}
}

func TestArtifactReaderPropagatesCompactedAndUnavailableStatesFailClosed(t *testing.T) {
	ref := core.ArtifactInputRef(readerArtifactRef(t))
	for _, tc := range []struct {
		name string
		want error
	}{
		{"compacted", ErrArtifactInputCompacted}, {"unavailable", ErrArtifactInputUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &artifactInputStoreProbe{err: tc.want}
			reader, err := NewArtifactReader(&rawReaderProbe{}, store)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := reader.ReadInputRange(context.Background(), ref, 0, 1); !errors.Is(err, tc.want) {
				t.Fatalf("err=%v want=%v", err, tc.want)
			}
		})
	}
}

func TestArtifactReaderRejectsInvalidRangeBeforeStoreAccess(t *testing.T) {
	store := &artifactInputStoreProbe{data: []byte("x")}
	reader, err := NewArtifactReader(&rawReaderProbe{}, store)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reader.ReadInputRange(context.Background(), core.ArtifactInputRef(readerArtifactRef(t)), -1, 1); err == nil {
		t.Fatal("negative artifact range accepted")
	}
	if store.calls != 0 {
		t.Fatalf("invalid range reached store calls=%d", store.calls)
	}
}
