package structuredresult

import (
	"context"
	"errors"
	"fmt"

	core "github.com/maemreyo/shellbeam/internal/core/structuredresult"
)

var (
	ErrArtifactInputCompacted   = errors.New("artifact_input_compacted")
	ErrArtifactInputUnavailable = errors.New("artifact_input_unavailable")
)

type ArtifactReader struct {
	raw       Reader
	artifacts ArtifactInputStore
}

func NewArtifactReader(raw Reader, artifacts ArtifactInputStore) (*ArtifactReader, error) {
	if raw == nil || artifacts == nil {
		return nil, fmt.Errorf("structured artifact reader unavailable")
	}
	return &ArtifactReader{raw: raw, artifacts: artifacts}, nil
}

func (r *ArtifactReader) ReadInputRange(ctx context.Context, input core.StructuredInputRef, offset int64, max int) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r == nil || r.raw == nil || r.artifacts == nil || input.Validate() != nil || offset < 0 || max < 0 {
		return nil, fmt.Errorf("invalid structured input range")
	}
	if _, ok := input.Raw(); ok {
		return r.raw.ReadInputRange(ctx, input, offset, max)
	}
	if input.Kind != core.StructuredInputArtifactBlob || input.ArtifactBlob == nil {
		return nil, ErrArtifactInputUnavailable
	}
	ref := *input.ArtifactBlob
	if offset > ref.Size {
		return nil, fmt.Errorf("invalid artifact input range")
	}
	remaining := ref.Size - offset
	if remaining == 0 || max == 0 {
		return []byte{}, nil
	}
	if int64(max) > remaining {
		max = int(remaining)
	}
	return r.artifacts.ReadArtifactBlobRange(ctx, ref, offset, max)
}

func (r *ArtifactReader) DescribeInput(ctx context.Context, input core.StructuredInputRef) (InputContext, error) {
	if err := ctx.Err(); err != nil {
		return InputContext{}, err
	}
	if r == nil || r.raw == nil || r.artifacts == nil || input.Validate() != nil {
		return InputContext{}, fmt.Errorf("invalid structured input")
	}
	if _, ok := input.Raw(); ok {
		return r.raw.DescribeInput(ctx, input)
	}
	if input.Kind != core.StructuredInputArtifactBlob || input.ArtifactBlob == nil {
		return InputContext{}, ErrArtifactInputUnavailable
	}
	return InputContext{OperationID: input.ArtifactBlob.OperationID}, nil
}
