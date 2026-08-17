package daemon

import (
	"context"
	"strconv"
	"time"

	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/media"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

type MediaReader interface {
	Read(context.Context, string, media.LogicalPath, media.Limits) (media.File, error)
}

type MediaRequest struct {
	WorkspaceID string `json:"workspace_id,omitempty"`
	CWD         string `json:"cwd,omitempty"`
	Path        string `json:"path"`
}

type mediaWorkResult struct {
	result media.Result
	err    error
}

func (s *Service) ReadMedia(ctx context.Context, req MediaRequest) (media.Result, error) {
	path, address, err := validateMediaRequest(req)
	if err != nil {
		return media.Result{}, err
	}
	if s.options.MediaReader == nil {
		return media.Result{}, failure.New(failure.FeatureUnavailable, map[string]string{"feature": "rich_local_media"}, nil)
	}
	if err := s.acquireMediaSlot(); err != nil {
		return media.Result{}, err
	}

	workerCtx, cancel := context.WithCancel(ctx)
	results := make(chan mediaWorkResult, 1)
	go s.runMediaWorker(workerCtx, req, path, address, results)

	after := s.mediaAfter
	if after == nil {
		after = time.After
	}
	select {
	case result := <-results:
		cancel()
		return result.result, result.err
	case <-after(s.mediaReadBudget):
		cancel()
		return media.Result{}, failure.New(failure.MediaReadTimeout, nil, nil)
	case <-ctx.Done():
		cancel()
		return media.Result{}, ctx.Err()
	}
}

func validateMediaRequest(req MediaRequest) (media.LogicalPath, media.DisplayAddress, error) {
	path, err := media.ParseLogicalPath(req.Path)
	if err != nil {
		return media.LogicalPath{}, media.DisplayAddress{}, failure.New(failure.InvalidInput, map[string]string{"field": "path"}, err)
	}
	var address media.DisplayAddress
	switch {
	case req.WorkspaceID != "" && req.CWD == "":
		if _, err := workspace.ParseWorkspaceID(req.WorkspaceID); err != nil {
			return media.LogicalPath{}, media.DisplayAddress{}, failure.New(failure.InvalidInput, map[string]string{"field": "workspace_id"}, err)
		}
		address = media.DisplayAddress{AddressKind: media.AddressWorkspace, WorkspaceID: req.WorkspaceID, Path: req.Path}
	case req.WorkspaceID == "" && req.CWD != "":
		if err := media.ValidateCWD(req.CWD); err != nil {
			return media.LogicalPath{}, media.DisplayAddress{}, failure.New(failure.InvalidInput, map[string]string{"field": "cwd"}, err)
		}
		address = media.DisplayAddress{AddressKind: media.AddressCWD, CWD: req.CWD, Path: req.Path}
	default:
		return media.LogicalPath{}, media.DisplayAddress{}, failure.New(failure.InvalidInput, map[string]string{"field": "address"}, nil)
	}
	if err := address.Validate(); err != nil {
		return media.LogicalPath{}, media.DisplayAddress{}, failure.New(failure.InvalidInput, map[string]string{"field": "address"}, err)
	}
	return path, address, nil
}

func (s *Service) acquireMediaSlot() error {
	select {
	case s.mediaSlots <- struct{}{}:
		return nil
	default:
		return failure.New(failure.CapacityExceeded, map[string]string{
			"active": strconv.Itoa(len(s.mediaSlots)), "limit": strconv.Itoa(cap(s.mediaSlots)),
		}, nil)
	}
}

func (s *Service) runMediaWorker(ctx context.Context, req MediaRequest, path media.LogicalPath, address media.DisplayAddress, out chan<- mediaWorkResult) {
	defer func() {
		<-s.mediaSlots
		if s.mediaWorkerDone != nil {
			s.mediaWorkerDone <- struct{}{}
		}
	}()
	base, err := s.resolveMediaBase(ctx, req)
	if err != nil {
		out <- mediaWorkResult{err: err}
		return
	}
	file, err := s.options.MediaReader.Read(ctx, base, path, media.V1Limits())
	if err != nil {
		out <- mediaWorkResult{err: err}
		return
	}
	data := append([]byte(nil), file.Data...)
	out <- mediaWorkResult{result: media.Result{
		SchemaVersion: 1, Kind: "media", DisplayAddress: address,
		MIMEType: file.MIMEType, Format: file.Format, ByteSize: len(data), Width: file.Width, Height: file.Height, Data: data,
	}}
}

func (s *Service) resolveMediaBase(ctx context.Context, req MediaRequest) (string, error) {
	if req.CWD != "" {
		return req.CWD, nil
	}
	if s.resolver == nil {
		return "", failure.New(failure.WorkspaceNotFound, map[string]string{"workspace_id": req.WorkspaceID}, nil)
	}
	resolved, err := s.resolver.ResolveAddress(ctx, workspace.Address{WorkspaceID: workspace.WorkspaceID(req.WorkspaceID)})
	if err != nil {
		return "", err
	}
	return resolved.CWD, nil
}
