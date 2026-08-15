package project

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/maemreyo/shellbeam/internal/core/failure"
	core "github.com/maemreyo/shellbeam/internal/core/project"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

type fakePathValidator struct {
	calls int
	err   error
}

func (f *fakePathValidator) ValidatePath(_ context.Context, _ workspace.Workspace, _ core.ParameterDefinition, value string) (ParameterValidation, error) {
	f.calls++
	if f.err != nil {
		return ParameterValidation{}, f.err
	}
	return ParameterValidation{Value: filepath.Clean(value), ObservationQuality: core.PathObservationExactAtBind}, nil
}

type fakePackageValidator struct {
	calls int
	err   error
}

func (f *fakePackageValidator) ValidatePackage(_ context.Context, _ workspace.Workspace, provider, value string) (ParameterValidation, error) {
	f.calls++
	if f.err != nil {
		return ParameterValidation{}, f.err
	}
	return ParameterValidation{Value: value, ProviderID: provider + "-repo-package", ProviderVersion: 1}, nil
}

func TestBinderBindsAllParameterKindsDefaultsAndCanonicalInteger(t *testing.T) {
	load := bindProjectLoad(t, `schema_version=2
[commands.test]
argv=["tool","{name}","{mode}","{count}","{path}","{pkg}"]
cwd="sub"
[commands.test.params.name]
kind="string"
required=true
[commands.test.params.mode]
kind="enum"
required=false
default="-fast"
enum=["-fast","safe"]
[commands.test.params.count]
kind="integer"
required=true
min=1
max=10
[commands.test.params.path]
kind="repo_path"
required=true
exists="file"
[commands.test.params.pkg]
kind="repo_package"
required=true
provider="go"
`)
	pathValidator := &fakePathValidator{}
	packageValidator := &fakePackageValidator{}
	binder := NewBinder(fakeWorkspaceLookup{values: []workspace.Workspace{bindWorkspace()}}, &fakeLoader{result: load}, pathValidator, packageValidator)
	got, err := binder.Bind(context.Background(), BindRequest{
		WorkspaceID: string(bindWorkspace().ID), CommandID: "test",
		Params: map[string]string{"name": "alpha", "count": "+003", "path": "pkg/../pkg/file.go", "pkg": "./internal/app"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"tool", "alpha", "-fast", "3", "pkg/file.go", "./internal/app"}
	if strings.Join(got.ResolvedArgv, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("argv=%q want=%q", got.ResolvedArgv, want)
	}
	if got.LogicalCWD != "sub" || got.ResolvedCWD != filepath.Join(bindWorkspace().Root, "sub") {
		t.Fatalf("cwd logical=%q resolved=%q", got.LogicalCWD, got.ResolvedCWD)
	}
	if got.ManifestDigest != load.ManifestDigest || got.ManifestSchemaVersion != core.ManifestSchemaV2 || got.CommandID != "test" || len(got.ParameterFingerprint) != 64 {
		t.Fatalf("binding identity=%#v", got)
	}
	if pathValidator.calls != 1 || packageValidator.calls != 1 || got.PathObservationQuality != core.PathObservationExactAtBind {
		t.Fatalf("validators path=%d package=%d quality=%q", pathValidator.calls, packageValidator.calls, got.PathObservationQuality)
	}
	if len(got.Parameters) != 5 {
		t.Fatalf("parameters=%#v", got.Parameters)
	}
	for i := 1; i < len(got.Parameters); i++ {
		if got.Parameters[i-1].ID >= got.Parameters[i].ID {
			t.Fatalf("parameters not sorted: %#v", got.Parameters)
		}
	}
	byID := map[string]core.ParameterBinding{}
	for _, value := range got.Parameters {
		byID[value.ID] = value
	}
	if byID["mode"].Source != core.BindingSourceDefault || byID["count"].Value != "3" || byID["count"].Source != core.BindingSourceCaller {
		t.Fatalf("parameter sources/canonicalization=%#v", byID)
	}
	if byID["pkg"].ProviderID != "go-repo-package" || byID["pkg"].ProviderVersion != 1 {
		t.Fatalf("package provider=%#v", byID["pkg"])
	}
}

func TestBinderRejectsUnknownMissingInvalidAndPositionalLeadingDash(t *testing.T) {
	load := bindProjectLoad(t, `schema_version=2
[commands.test]
argv=["tool","{name}","{count}"]
cwd="."
[commands.test.params.name]
kind="string"
required=true
[commands.test.params.count]
kind="integer"
required=true
min=1
max=2
`)
	binder := NewBinder(fakeWorkspaceLookup{values: []workspace.Workspace{bindWorkspace()}}, &fakeLoader{result: load}, &fakePathValidator{}, &fakePackageValidator{})
	cases := []BindRequest{
		{WorkspaceID: string(bindWorkspace().ID), CommandID: "test", Params: map[string]string{"name": "ok", "count": "1", "extra": "x"}},
		{WorkspaceID: string(bindWorkspace().ID), CommandID: "test", Params: map[string]string{"count": "1"}},
		{WorkspaceID: string(bindWorkspace().ID), CommandID: "test", Params: map[string]string{"name": "ok", "count": "3"}},
		{WorkspaceID: string(bindWorkspace().ID), CommandID: "test", Params: map[string]string{"name": "-flag", "count": "1"}},
	}
	for _, request := range cases {
		if got, err := binder.Bind(context.Background(), request); err == nil {
			t.Fatalf("accepted request=%#v binding=%#v", request, got)
		}
	}
}

func TestBinderAllowsExactOptionLikeEnumButNotArbitraryOptionShape(t *testing.T) {
	load := bindProjectLoad(t, `schema_version=2
[commands.test]
argv=["tool","{mode}"]
cwd="."
[commands.test.params.mode]
kind="enum"
required=true
enum=["-run","safe"]
`)
	binder := NewBinder(fakeWorkspaceLookup{values: []workspace.Workspace{bindWorkspace()}}, &fakeLoader{result: load}, &fakePathValidator{}, &fakePackageValidator{})
	got, err := binder.Bind(context.Background(), BindRequest{WorkspaceID: string(bindWorkspace().ID), CommandID: "test", Params: map[string]string{"mode": "-run"}})
	if err != nil || len(got.ResolvedArgv) != 2 || got.ResolvedArgv[1] != "-run" {
		t.Fatalf("exact option enum binding=%#v err=%v", got, err)
	}
	if _, err := binder.Bind(context.Background(), BindRequest{WorkspaceID: string(bindWorkspace().ID), CommandID: "test", Params: map[string]string{"mode": "-other"}}); err == nil {
		t.Fatal("undeclared option-shaped enum accepted")
	}
}

func TestBinderDeterministicAcrossCallerMapOrder(t *testing.T) {
	load := bindProjectLoad(t, `schema_version=2
[commands.test]
argv=["tool","{a}","{b}"]
cwd="."
[commands.test.params.a]
kind="string"
required=true
[commands.test.params.b]
kind="string"
required=true
`)
	binder := NewBinder(fakeWorkspaceLookup{values: []workspace.Workspace{bindWorkspace()}}, &fakeLoader{result: load}, &fakePathValidator{}, &fakePackageValidator{})
	a, err := binder.Bind(context.Background(), BindRequest{WorkspaceID: string(bindWorkspace().ID), CommandID: "test", Params: map[string]string{"a": "one", "b": "two"}})
	if err != nil {
		t.Fatal(err)
	}
	b, err := binder.Bind(context.Background(), BindRequest{WorkspaceID: string(bindWorkspace().ID), CommandID: "test", Params: map[string]string{"b": "two", "a": "one"}})
	if err != nil {
		t.Fatal(err)
	}
	if a.ParameterFingerprint != b.ParameterFingerprint || strings.Join(a.ResolvedArgv, "\x00") != strings.Join(b.ResolvedArgv, "\x00") {
		t.Fatalf("non-deterministic a=%#v b=%#v", a, b)
	}
}

func TestBinderRejectsNonV2UnknownUnparameterizedAndParameterizedShell(t *testing.T) {
	v1 := bindProjectLoad(t, "schema_version=1\n[commands.test]\nargv=[\"true\"]\n")
	if _, err := NewBinder(fakeWorkspaceLookup{values: []workspace.Workspace{bindWorkspace()}}, &fakeLoader{result: v1}, &fakePathValidator{}, &fakePackageValidator{}).Bind(context.Background(), BindRequest{WorkspaceID: string(bindWorkspace().ID), CommandID: "test"}); err == nil {
		t.Fatal("v1 manifest accepted typed bind")
	}
	unparameterized := bindProjectLoad(t, "schema_version=2\n[commands.test]\nargv=[\"true\"]\ncwd=\".\"\n")
	binder := NewBinder(fakeWorkspaceLookup{values: []workspace.Workspace{bindWorkspace()}}, &fakeLoader{result: unparameterized}, &fakePathValidator{}, &fakePackageValidator{})
	if _, err := binder.Bind(context.Background(), BindRequest{WorkspaceID: string(bindWorkspace().ID), CommandID: "missing"}); err == nil {
		t.Fatal("unknown command accepted")
	}
	if _, err := binder.Bind(context.Background(), BindRequest{WorkspaceID: string(bindWorkspace().ID), CommandID: "test"}); err == nil {
		t.Fatal("unparameterized command accepted")
	}
	// Parser rejects parameterized shell manifests; binder must never invent shell interpolation.
	if _, err := core.Parse([]byte("schema_version=2\n[commands.test]\nshell=\"echo {name}\"\ncwd=\".\"\n[commands.test.params.name]\nkind=\"string\"\nrequired=true\n")); err == nil {
		t.Fatal("parameterized shell manifest unexpectedly parsed")
	}
}

func TestBinderFailsClosedWhenRepoPackageProviderUnavailable(t *testing.T) {
	load := bindProjectLoad(t, `schema_version=2
[commands.test]
argv=["go","test","{pkg}"]
cwd="."
[commands.test.params.pkg]
kind="repo_package"
required=true
provider="go"
`)
	provider := &fakePackageValidator{err: errors.New("provider unavailable")}
	binder := NewBinder(fakeWorkspaceLookup{values: []workspace.Workspace{bindWorkspace()}}, &fakeLoader{result: load}, &fakePathValidator{}, provider)
	if got, err := binder.Bind(context.Background(), BindRequest{WorkspaceID: string(bindWorkspace().ID), CommandID: "test", Params: map[string]string{"pkg": "./internal/app"}}); err == nil {
		t.Fatalf("provider failure accepted: %#v", got)
	}
	if provider.calls != 1 {
		t.Fatalf("provider calls=%d", provider.calls)
	}
}

func TestBinderDetectsManifestChangeAfterValidationBeforeFreeze(t *testing.T) {
	first := bindProjectLoad(t, `schema_version=2
[commands.test]
argv=["tool","{path}"]
cwd="."
[commands.test.params.path]
kind="repo_path"
required=true
exists="file"
`)
	second := first
	second.ManifestDigest = strings.Repeat("b", 64)
	loader := &sequenceProjectLoader{results: []core.LoadResult{first, second}}
	binder := NewBinder(fakeWorkspaceLookup{values: []workspace.Workspace{bindWorkspace()}}, loader, &fakePathValidator{}, &fakePackageValidator{})
	_, err := binder.Bind(context.Background(), BindRequest{WorkspaceID: string(bindWorkspace().ID), CommandID: "test", Params: map[string]string{"path": "file.go"}})
	if !core.HasCode(err, core.CodeChangedDuringResolve) {
		t.Fatalf("manifest race err=%v", err)
	}
	if loader.calls != 2 {
		t.Fatalf("loader calls=%d", loader.calls)
	}
}

func TestBinderDoesNotExecuteDependsOnCommands(t *testing.T) {
	load := bindProjectLoad(t, `schema_version=2
[commands.setup]
argv=["false"]
cwd="."
[commands.test]
argv=["tool","{name}"]
cwd="."
depends_on=["setup"]
[commands.test.params.name]
kind="string"
required=true
`)
	binder := NewBinder(fakeWorkspaceLookup{values: []workspace.Workspace{bindWorkspace()}}, &fakeLoader{result: load}, &fakePathValidator{}, &fakePackageValidator{})
	got, err := binder.Bind(context.Background(), BindRequest{WorkspaceID: string(bindWorkspace().ID), CommandID: "test", Params: map[string]string{"name": "ok"}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got.ResolvedArgv, " ") != "tool ok" {
		t.Fatalf("binding unexpectedly incorporated dependency: %#v", got)
	}
}

func bindProjectLoad(t *testing.T, raw string) core.LoadResult {
	t.Helper()
	parsed, err := core.Parse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	return core.LoadResult{State: core.LoadValid, Parsed: &parsed, ManifestDigest: core.RawDigest([]byte(raw)), DiscoveryFingerprint: parsed.Fingerprint}
}

func bindWorkspace() workspace.Workspace {
	value := testProjectWorkspace()
	value.Root = "/repo"
	return value
}

func FuzzBindIntegerCanonicalization(f *testing.F) {
	for _, seed := range []string{"0", "+003", "-7", "1000", "1001", "01", "not-int"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		min, max := int64(-1000), int64(1000)
		definition := core.ParameterDefinition{Kind: core.ParameterInteger, Required: true, Min: &min, Max: &max}
		got, err := bindInteger(definition, raw)
		parsed, parseErr := strconv.ParseInt(raw, 10, 64)
		valid := parseErr == nil && parsed >= min && parsed <= max
		if !valid {
			if err == nil {
				t.Fatalf("invalid integer %q accepted as %q", raw, got)
			}
			return
		}
		if err != nil || got != strconv.FormatInt(parsed, 10) {
			t.Fatalf("valid integer %q => %q err=%v", raw, got, err)
		}
	})
}

func FuzzBindStringBoundsUnicodeAndOptionShape(f *testing.F) {
	for _, seed := range []struct {
		value string
		allow bool
	}{{"ok", false}, {"-flag", false}, {"-flag", true}, {"line\nbreak", true}, {strings.Repeat("x", core.MaxStringBytes+1), true}} {
		f.Add(seed.value, seed.allow)
	}
	f.Fuzz(func(t *testing.T, raw string, allowLeadingDash bool) {
		definition := core.ParameterDefinition{Kind: core.ParameterString, Required: true, AllowLeadingDash: allowLeadingDash}
		got, err := bindString(definition, raw)
		valid := raw != "" && len(raw) <= core.MaxStringBytes && utf8.ValidString(raw)
		for _, r := range raw {
			if r == 0 || unicode.IsControl(r) {
				valid = false
				break
			}
		}
		if !allowLeadingDash && strings.HasPrefix(raw, "-") {
			valid = false
		}
		if !valid {
			if err == nil {
				t.Fatalf("invalid scalar %q accepted as %q", raw, got)
			}
			return
		}
		if err != nil || got != raw {
			t.Fatalf("valid scalar %q => %q err=%v", raw, got, err)
		}
	})
}

func TestBinderReturnsStableTypedCommandFailures(t *testing.T) {
	workspaceLookup := fakeWorkspaceLookup{values: []workspace.Workspace{bindWorkspace()}}
	valid := bindProjectLoad(t, `schema_version=2
[commands.test]
argv=["tool","{name}"]
cwd="."
[commands.test.params.name]
kind="string"
required=true
`)
	cases := []struct {
		name    string
		binder  *Binder
		request BindRequest
		code    failure.Code
	}{
		{"not found", NewBinder(workspaceLookup, &fakeLoader{result: valid}, &fakePathValidator{}, &fakePackageValidator{}), BindRequest{WorkspaceID: string(bindWorkspace().ID), CommandID: "missing"}, failure.ProjectCommandNotFound},
		{"missing", NewBinder(workspaceLookup, &fakeLoader{result: valid}, &fakePathValidator{}, &fakePackageValidator{}), BindRequest{WorkspaceID: string(bindWorkspace().ID), CommandID: "test"}, failure.ParameterMissing},
		{"unknown", NewBinder(workspaceLookup, &fakeLoader{result: valid}, &fakePathValidator{}, &fakePackageValidator{}), BindRequest{WorkspaceID: string(bindWorkspace().ID), CommandID: "test", Params: map[string]string{"name": "ok", "extra": "x"}}, failure.ParameterUnknown},
		{"invalid", NewBinder(workspaceLookup, &fakeLoader{result: valid}, &fakePathValidator{}, &fakePackageValidator{}), BindRequest{WorkspaceID: string(bindWorkspace().ID), CommandID: "test", Params: map[string]string{"name": "line\nbreak"}}, failure.ParameterInvalid},
	}
	unparameterized := bindProjectLoad(t, "schema_version=2\n[commands.test]\nargv=[\"true\"]\ncwd=\".\"\n")
	cases = append(cases, struct {
		name    string
		binder  *Binder
		request BindRequest
		code    failure.Code
	}{"not parameterized", NewBinder(workspaceLookup, &fakeLoader{result: unparameterized}, &fakePathValidator{}, &fakePackageValidator{}), BindRequest{WorkspaceID: string(bindWorkspace().ID), CommandID: "test"}, failure.ProjectCommandNotParameterized})
	packageLoad := bindProjectLoad(t, `schema_version=2
[commands.test]
argv=["go","test","{pkg}"]
cwd="."
[commands.test.params.pkg]
kind="repo_package"
required=true
provider="go"
`)
	cases = append(cases, struct {
		name    string
		binder  *Binder
		request BindRequest
		code    failure.Code
	}{"validation unavailable", NewBinder(workspaceLookup, &fakeLoader{result: packageLoad}, &fakePathValidator{}, nil), BindRequest{WorkspaceID: string(bindWorkspace().ID), CommandID: "test", Params: map[string]string{"pkg": "./internal/app"}}, failure.ParameterValidationUnavailable})

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.binder.Bind(context.Background(), tc.request)
			if got := failure.Public(err).Code; got != tc.code {
				t.Fatalf("code=%q want=%q err=%v", got, tc.code, err)
			}
		})
	}
}
