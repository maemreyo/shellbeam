package project

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/maemreyo/shellbeam/internal/core/failure"
	core "github.com/maemreyo/shellbeam/internal/core/project"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

type BindRequest struct {
	WorkspaceID string
	CommandID   string
	Params      map[string]string
	TimeoutMS   int64
	TTY         bool
}

type Binder struct {
	workspaces       WorkspaceLookup
	loader           Loader
	pathValidator    RepoPathValidator
	packageValidator RepoPackageValidator
}

func NewBinder(workspaces WorkspaceLookup, loader Loader, pathValidator RepoPathValidator, packageValidator RepoPackageValidator) *Binder {
	return &Binder{workspaces: workspaces, loader: loader, pathValidator: pathValidator, packageValidator: packageValidator}
}

func (b *Binder) Bind(ctx context.Context, request BindRequest) (core.CommandBinding, error) {
	if b == nil || b.workspaces == nil || b.loader == nil {
		return core.CommandBinding{}, fmt.Errorf("project binder unavailable")
	}
	if request.TimeoutMS < 0 {
		return core.CommandBinding{}, failure.New(failure.InvalidInput, map[string]string{"field": "timeout_ms"}, nil)
	}
	record, err := b.workspace(ctx, request.WorkspaceID)
	if err != nil {
		return core.CommandBinding{}, err
	}
	load := b.loader.Load(ctx, record.Root)
	manifest, command, err := bindTarget(load, request.CommandID)
	if err != nil {
		return core.CommandBinding{}, err
	}
	parameters, values, pathQuality, err := b.bindParameters(ctx, record, request.CommandID, command, request.Params)
	if err != nil {
		return core.CommandBinding{}, err
	}
	resolvedArgv := resolveArgv(command.Argv, values)
	resolvedCWD, err := resolveCommandCWD(record.Root, command.CWD)
	if err != nil {
		return core.CommandBinding{}, err
	}
	if err := b.confirmManifest(ctx, record.Root, load); err != nil {
		return core.CommandBinding{}, err
	}
	fingerprint, err := core.ParameterFingerprint(parameters)
	if err != nil {
		return core.CommandBinding{}, err
	}
	binding := core.CommandBinding{
		SchemaVersion: core.BindingSchemaVersion, ManifestDigest: load.ManifestDigest,
		ManifestSchemaVersion: manifest.SchemaVersion, CommandID: request.CommandID,
		ParameterFingerprint: fingerprint, Parameters: parameters, ResolvedArgv: resolvedArgv,
		LogicalCWD: command.CWD, ResolvedCWD: resolvedCWD, PathObservationQuality: pathQuality,
		Kind: command.Kind, SourceScope: command.SourceScope,
		ExpectedOutputs: append([]core.Output(nil), command.ExpectedOutputs...),
	}
	if err := binding.Validate(); err != nil {
		return core.CommandBinding{}, err
	}
	return binding, nil
}

func bindTarget(load core.LoadResult, commandID string) (core.Manifest, core.Command, error) {
	if load.State != core.LoadValid || load.Parsed == nil || load.ManifestDigest == "" {
		return core.Manifest{}, core.Command{}, failure.New(failure.ProjectCommandNotFound, map[string]string{"command": commandID}, fmt.Errorf("project manifest unavailable"))
	}
	manifest := load.Parsed.Manifest
	if manifest.SchemaVersion != core.ManifestSchemaV2 {
		return core.Manifest{}, core.Command{}, failure.New(failure.ProjectCommandNotParameterized, map[string]string{"command": commandID}, fmt.Errorf("typed project commands require manifest v2"))
	}
	command, ok := manifest.Commands[commandID]
	if !ok || commandID == "" {
		return core.Manifest{}, core.Command{}, failure.New(failure.ProjectCommandNotFound, map[string]string{"command": commandID}, fmt.Errorf("project command not found"))
	}
	if len(command.Argv) == 0 || command.Shell != "" {
		return core.Manifest{}, core.Command{}, failure.New(failure.ProjectCommandNotParameterized, map[string]string{"command": commandID}, fmt.Errorf("typed project command must use argv"))
	}
	return manifest, command, nil
}

func (b *Binder) bindParameters(ctx context.Context, record workspace.Workspace, commandID string, command core.Command, supplied map[string]string) ([]core.ParameterBinding, map[string]string, string, error) {
	for id := range supplied {
		if _, ok := command.Params[id]; !ok {
			return nil, nil, "", failure.New(failure.ParameterUnknown, map[string]string{"command": commandID, "parameter": id}, fmt.Errorf("unknown parameter"))
		}
	}
	ids := make([]string, 0, len(command.Params))
	for id := range command.Params {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var bindings []core.ParameterBinding
	values := make(map[string]string, len(ids))
	pathQuality := ""
	for _, id := range ids {
		binding, quality, err := b.bindParameter(ctx, record, commandID, id, command.Params[id], supplied)
		if err != nil {
			return nil, nil, "", err
		}
		bindings = append(bindings, binding)
		values[id] = binding.Value
		if quality != "" {
			pathQuality = quality
		}
	}
	return bindings, values, pathQuality, nil
}

func (b *Binder) bindParameter(ctx context.Context, record workspace.Workspace, commandID, id string, definition core.ParameterDefinition, supplied map[string]string) (core.ParameterBinding, string, error) {
	value, source, err := parameterInput(id, definition, supplied)
	if err != nil {
		return core.ParameterBinding{}, "", failure.New(failure.ParameterMissing, map[string]string{"command": commandID, "parameter": id}, err)
	}
	result := ParameterValidation{Value: value}
	switch definition.Kind {
	case core.ParameterString:
		result.Value, err = bindString(definition, value)
	case core.ParameterEnum:
		result.Value, err = bindEnum(definition, value)
	case core.ParameterInteger:
		result.Value, err = bindInteger(definition, value)
	case core.ParameterRepoPath:
		if b.pathValidator == nil {
			err = failure.New(failure.ParameterValidationUnavailable, map[string]string{"command": commandID, "parameter": id, "kind": string(definition.Kind)}, fmt.Errorf("repo path validator unavailable"))
		} else {
			result, err = b.pathValidator.ValidatePath(ctx, record, definition, value)
		}
	case core.ParameterRepoPackage:
		if b.packageValidator == nil {
			err = failure.New(failure.ParameterValidationUnavailable, map[string]string{"command": commandID, "parameter": id, "kind": string(definition.Kind), "provider": definition.Provider}, fmt.Errorf("repo package validator unavailable"))
		} else {
			result, err = b.packageValidator.ValidatePackage(ctx, record, definition.Provider, value)
		}
	default:
		err = failure.New(failure.ParameterKindUnsupported, map[string]string{"command": commandID, "parameter": id, "kind": string(definition.Kind)}, fmt.Errorf("unsupported parameter kind"))
	}
	if err != nil {
		public := failure.Public(err)
		if public.Code != failure.Internal {
			return core.ParameterBinding{}, "", err
		}
		code := failure.ParameterInvalid
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			code = failure.ParameterValidationUnavailable
		}
		return core.ParameterBinding{}, "", failure.New(code, map[string]string{"command": commandID, "parameter": id, "kind": string(definition.Kind), "provider": definition.Provider}, err)
	}
	binding := core.ParameterBinding{ID: id, Kind: definition.Kind, Value: result.Value, Source: source, ProviderID: result.ProviderID, ProviderVersion: result.ProviderVersion}
	if err := binding.Validate(); err != nil {
		return core.ParameterBinding{}, "", err
	}
	return binding, result.ObservationQuality, nil
}

func parameterInput(id string, definition core.ParameterDefinition, supplied map[string]string) (string, string, error) {
	if value, ok := supplied[id]; ok {
		return value, core.BindingSourceCaller, nil
	}
	if definition.Required {
		return "", "", fmt.Errorf("missing parameter %q", id)
	}
	return definition.Default, core.BindingSourceDefault, nil
}

func bindString(definition core.ParameterDefinition, value string) (string, error) {
	if !validScalar(value) || !definition.AllowLeadingDash && strings.HasPrefix(value, "-") {
		return "", fmt.Errorf("invalid string parameter")
	}
	return value, nil
}

func bindEnum(definition core.ParameterDefinition, value string) (string, error) {
	if !validScalar(value) {
		return "", fmt.Errorf("invalid enum parameter")
	}
	for _, candidate := range definition.Enum {
		if value == candidate {
			return value, nil
		}
	}
	return "", fmt.Errorf("enum value is not declared")
}

func bindInteger(definition core.ParameterDefinition, value string) (string, error) {
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || definition.Min != nil && parsed < *definition.Min || definition.Max != nil && parsed > *definition.Max {
		return "", fmt.Errorf("invalid integer parameter")
	}
	return strconv.FormatInt(parsed, 10), nil
}

func validScalar(value string) bool {
	if value == "" || len(value) > core.MaxStringBytes || !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if r == 0 || unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func resolveArgv(argv []string, values map[string]string) []string {
	out := append([]string(nil), argv...)
	for index, token := range out {
		if len(token) >= 3 && token[0] == '{' && token[len(token)-1] == '}' {
			if value, ok := values[token[1:len(token)-1]]; ok {
				out[index] = value
			}
		}
	}
	return out
}

func resolveCommandCWD(root, logical string) (string, error) {
	resolved := filepath.Clean(filepath.Join(root, filepath.FromSlash(logical)))
	rel, err := filepath.Rel(filepath.Clean(root), resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("project command cwd escapes workspace")
	}
	return resolved, nil
}

func (b *Binder) confirmManifest(ctx context.Context, root string, original core.LoadResult) error {
	confirmed := b.loader.Load(ctx, root)
	if confirmed.State != core.LoadValid || confirmed.Parsed == nil || confirmed.ManifestDigest != original.ManifestDigest || confirmed.Parsed.Manifest.SchemaVersion != original.Parsed.Manifest.SchemaVersion {
		return core.ChangedDuringResolveError()
	}
	return nil
}

func (b *Binder) workspace(ctx context.Context, workspaceID string) (workspace.Workspace, error) {
	id, err := workspace.ParseWorkspaceID(workspaceID)
	if err != nil {
		return workspace.Workspace{}, failure.New(failure.InvalidInput, map[string]string{"field": "workspace_id"}, err)
	}
	records, err := b.workspaces.ListWorkspaces(ctx)
	if err != nil {
		return workspace.Workspace{}, failure.New(failure.PersistenceUnavailable, nil, err)
	}
	for _, record := range records {
		if record.ID == id {
			return record, nil
		}
	}
	return workspace.Workspace{}, failure.New(failure.WorkspaceNotFound, map[string]string{"workspace_id": workspaceID}, nil)
}
