package verification

import (
	"context"
	"testing"

	projectapp "github.com/maemreyo/shellbeam/internal/app/project"
	project "github.com/maemreyo/shellbeam/internal/core/project"
)

type fakeProjectBinder struct {
	request projectapp.BindRequest
	result  project.CommandBinding
	err     error
}

func (f *fakeProjectBinder) Bind(_ context.Context, req projectapp.BindRequest) (project.CommandBinding, error) {
	f.request = req
	return f.result, f.err
}

func TestProjectCommandSourceResolvesThroughExactTypedBinder(t *testing.T) {
	fake := &fakeProjectBinder{result: project.CommandBinding{CommandID: "check"}}
	source := NewProjectCommandSource(fake)
	params := map[string]string{"pkg": "./internal/app"}
	got, err := source.Resolve(context.Background(), "ws_01K00000000000000000000000", "check", params)
	if err != nil {
		t.Fatal(err)
	}
	if got.CommandID != "check" || fake.request.WorkspaceID != "ws_01K00000000000000000000000" || fake.request.CommandID != "check" || fake.request.Params["pkg"] != "./internal/app" {
		t.Fatalf("got=%#v request=%#v", got, fake.request)
	}
	params["pkg"] = "mutated"
	if fake.request.Params["pkg"] != "./internal/app" {
		t.Fatal("resolver aliased caller params")
	}
	if fake.request.TimeoutMS != 0 || fake.request.TTY {
		t.Fatalf("unexpected execution fields %#v", fake.request)
	}
}
