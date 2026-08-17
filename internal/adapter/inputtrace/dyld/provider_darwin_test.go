//go:build darwin

package dyld

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	trace "github.com/maemreyo/shellbeam/internal/core/inputtrace"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

func TestE27InterposeNativeCapturesRepresentativePartialClasses(t *testing.T) {
	state := e27PrivateState(t)
	provider := New(state)
	prepared, err := provider.Prepare(context.Background(), e27PrepareRequest("native"))
	if err != nil {
		t.Fatal(err)
	}
	binding := prepared.Binding()
	for _, coverage := range []trace.Coverage{
		binding.Coverage.FilesystemReads, binding.Coverage.FilesystemMetadataQueries, binding.Coverage.DirectoryEnumerations,
		binding.Coverage.FilesystemWrites, binding.Coverage.ExecutedBinaries, binding.Coverage.LoadedLibraries, binding.Coverage.ChildProcesses,
	} {
		if coverage != trace.CoveragePartial {
			t.Fatalf("overclaimed coverage=%q binding=%#v", coverage, binding)
		}
	}
	if binding.PreExecCoverageEstablished || binding.Coverage.EnvironmentNamesObserved != trace.CoverageUnsupported || binding.Coverage.NetworkAttempts != trace.CoverageUnsupported {
		t.Fatalf("truthful matrix violated: %#v", binding)
	}

	fixtureRoot := t.TempDir()
	readPath := filepath.Join(fixtureRoot, "read.txt")
	metaPath := filepath.Join(fixtureRoot, "meta.txt")
	if err := os.WriteFile(readPath, []byte("read"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(metaPath, []byte("meta"), 0600); err != nil {
		t.Fatal(err)
	}
	fixture := buildE27Fixture(t)
	cmd := exec.Command(fixture, fixtureRoot, readPath, metaPath)
	cmd.Env = mergeE27TestEnvironment(os.Environ(), prepared.EnvironmentAdditions())
	if output, err := cmd.CombinedOutput(); err != nil {
		_ = prepared.Abort()
		t.Fatalf("fixture err=%v output=%s", err, output)
	}
	snapshot, err := provider.Finalize(context.Background(), binding)
	if err != nil {
		t.Fatal(err)
	}
	classes := map[trace.ObservationClass]bool{}
	for _, resource := range snapshot.Resources {
		classes[resource.ObservationClass] = true
	}
	for _, want := range []trace.ObservationClass{trace.ClassFilesystemReads, trace.ClassFilesystemMetadataQueries, trace.ClassDirectoryEnumerations, trace.ClassFilesystemWrites, trace.ClassExecutedBinaries, trace.ClassLoadedLibraries} {
		if !classes[want] {
			t.Fatalf("missing class %q resources=%#v", want, snapshot.Resources)
		}
	}
	if snapshot.Coverage.ChildProcesses == trace.CoverageCompleteForOwnedTree {
		t.Fatal("native provider promoted child coverage to complete")
	}
}

func buildE27Fixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	source := filepath.Join(root, "fixture.c")
	binary := filepath.Join(root, "fixture")
	code := `
#include <dirent.h>
#include <dlfcn.h>
#include <fcntl.h>
#include <stdlib.h>
#include <stdio.h>
#include <sys/stat.h>
#include <sys/types.h>
#include <sys/wait.h>
#include <unistd.h>
int main(int argc, char **argv, char **envp) {
  if (argc != 4) return 20;
  int r = open(argv[2], O_RDONLY); if (r < 0) return 21; char b[8]; (void)read(r,b,sizeof(b)); close(r);
  struct stat st; if (stat(argv[3], &st) != 0) return 22;
  DIR *d = opendir(argv[1]); if (!d) return 23; (void)readdir(d); closedir(d);
  char out[4096]; snprintf(out,sizeof(out),"%s/write.txt",argv[1]); int w=open(out,O_WRONLY|O_CREAT|O_TRUNC,0600); if(w<0)return 24; (void)write(w,"x",1); close(w);
  void *h = dlopen("/usr/lib/libSystem.B.dylib", RTLD_LAZY); if (h) dlclose(h);
  pid_t p=fork(); if(p==0){char *a[]={(char*)"/usr/bin/true",NULL}; execve(a[0],a,envp); _exit(25);} if(p<0)return 26; int status=0; waitpid(p,&status,0);
  return 0;
}`
	if err := os.WriteFile(source, []byte(code), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("/usr/bin/clang", source, "-o", binary)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fixture compile: %v %s", err, output)
	}
	return binary
}

func mergeE27TestEnvironment(base []string, additions []operation.EnvironmentEntry) []string {
	values := map[string]string{}
	for _, entry := range base {
		if key, value, ok := strings.Cut(entry, "="); ok {
			values[key] = value
		}
	}
	for _, entry := range additions {
		values[entry.Key] = entry.Value
	}
	out := make([]string, 0, len(values))
	for key, value := range values {
		out = append(out, fmt.Sprintf("%s=%s", key, value))
	}
	return out
}
