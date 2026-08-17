//go:build darwin

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	ipcadapter "github.com/maemreyo/shellbeam/internal/adapter/ipc"
	traceapp "github.com/maemreyo/shellbeam/internal/app/inputtrace"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	trace "github.com/maemreyo/shellbeam/internal/core/inputtrace"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

func TestE27NativeDynamicInputTraceAcceptance(t *testing.T) {
	binary := buildB1NativeBinary(t)
	disabledState, disabledRun := b1NativeDirs(t)
	enabledState, enabledRun := b1NativeDirs(t)
	fixtureRoot := t.TempDir()
	externalRoot := t.TempDir()
	externalPath := filepath.Join(externalRoot, "LOWENTROPY-E27-EXTERNAL.txt")
	if err := os.WriteFile(externalPath, []byte("external"), 0o600); err != nil {
		t.Fatal(err)
	}
	secrets := map[string]string{
		"E27_PRIVATE_EXTERNAL": externalPath,
		"E27_ENV_SECRET":       "LOWENTROPY-E27-ENV-VALUE",
		"E27_NETWORK_PAYLOAD":  "LOWENTROPY-E27-NETWORK-PAYLOAD",
	}
	disabled := startE27NativeDaemon(t, binary, disabledState, disabledRun, false, nil)
	defer disabled.hardKill(t)
	enabled := startE27NativeDaemon(t, binary, enabledState, enabledRun, true, secrets)
	defer enabled.hardKill(t)

	assertE27NativeCatalog(t, disabled, false)
	assertE27NativeCatalog(t, enabled, true)
	assertE27NativeNoTax(t, disabled, enabled, fixtureRoot, enabledState)
	assertE27NativeRequiredAndUnsupported(t, enabled, fixtureRoot, enabledState)
	fixture := buildE27AcceptanceFixture(t)
	assertE27NativeBestEffortTrace(t, enabled, enabledState, fixture, fixtureRoot, secrets)
}

func startE27NativeDaemon(t *testing.T, binary, stateDir, runtimeDir string, enabled bool, extraEnv map[string]string) *b1NativeDaemon {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(stateDir), 0o700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(filepath.Dir(stateDir), "e27-config.toml")
	config := fmt.Sprintf("schema_version = 1\nmax_concurrent_sessions = 8\nexperimental_input_tracing = %t\n", enabled)
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(filepath.Dir(stateDir), "daemon.log")
	log, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "daemon", "--config", configPath, "--state-dir", stateDir, "--runtime-dir", runtimeDir, "--shell", "/bin/sh")
	cmd.Stdout, cmd.Stderr = log, log
	cmd.Env = append([]string(nil), os.Environ()...)
	for key, value := range extraEnv {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	if err := cmd.Start(); err != nil {
		_ = log.Close()
		t.Fatal(err)
	}
	d := &b1NativeDaemon{cmd: cmd, log: log, logPath: logPath, running: true}
	d.client = ipcadapter.NewClient(filepath.Join(runtimeDir, "daemon.sock"))
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		response, callErr := d.client.CallV2(context.Background(), e27Request(ipcadapter.RequestV2{Action: "inspect.server"}))
		if callErr == nil && response.OK {
			return d
		}
		time.Sleep(25 * time.Millisecond)
	}
	d.hardKill(t)
	data, _ := os.ReadFile(logPath)
	t.Fatalf("E27 daemon did not become ready: %s", data)
	return nil
}

func assertE27NativeCatalog(t *testing.T, daemon *b1NativeDaemon, enabled bool) {
	t.Helper()
	response := callB1NativeDaemon(t, daemon, ipcadapter.RequestV2{Action: "inspect.server"})
	if response.Server == nil {
		t.Fatal("inspect.server missing server info")
	}
	support := response.Server.InputTracing
	if !enabled {
		if support != nil {
			t.Fatalf("disabled daemon advertised input tracing: %#v", support)
		}
		return
	}
	if support == nil || support.Provider.ID != "dyld-interpose" || support.Authority != trace.AuthorityAdvisory || support.PreExecCoverage {
		t.Fatalf("enabled input tracing support=%#v", support)
	}
	for _, coverage := range []trace.Coverage{support.Coverage.FilesystemReads, support.Coverage.FilesystemMetadataQueries, support.Coverage.DirectoryEnumerations, support.Coverage.FilesystemWrites, support.Coverage.ExecutedBinaries, support.Coverage.LoadedLibraries, support.Coverage.ChildProcesses} {
		if coverage != trace.CoveragePartial {
			t.Fatalf("overclaimed catalog coverage=%q support=%#v", coverage, support)
		}
	}
	if support.Coverage.EnvironmentNamesObserved != trace.CoverageUnsupported || support.Coverage.NetworkAttempts != trace.CoverageUnsupported {
		t.Fatalf("unsupported coverage overclaimed: %#v", support.Coverage)
	}
}

func assertE27NativeNoTax(t *testing.T, disabled, enabled *b1NativeDaemon, cwd, enabledState string) {
	t.Helper()
	const samples = 200
	disabledDurations := make([]time.Duration, 0, samples)
	enabledDurations := make([]time.Duration, 0, samples)
	for i := 0; i < samples; i++ {
		disabledID := fmt.Sprintf("e27-accept-disabled-%03d", i)
		enabledID := fmt.Sprintf("e27-accept-enabled-%03d", i)
		if i%2 == 0 {
			disabledDurations = append(disabledDurations, measureE27NativeAdmission(t, disabled.client, disabledID, cwd))
			enabledDurations = append(enabledDurations, measureE27NativeAdmission(t, enabled.client, enabledID, cwd))
		} else {
			enabledDurations = append(enabledDurations, measureE27NativeAdmission(t, enabled.client, enabledID, cwd))
			disabledDurations = append(disabledDurations, measureE27NativeAdmission(t, disabled.client, disabledID, cwd))
		}
	}
	inc95, inc99, ok := e26BatchedIncrementPercentiles(disabledDurations, enabledDurations, 20)
	if !ok {
		t.Fatal("invalid E27 no-tax sample shape")
	}
	t.Logf("E27 enabled-unused admission disabled p95=%s p99=%s enabled p95=%s p99=%s batched incremental p95=%s p99=%s", e26Percentile(disabledDurations, 95), e26Percentile(disabledDurations, 99), e26Percentile(enabledDurations, 95), e26Percentile(enabledDurations, 99), inc95, inc99)
	if slow, limit := e26AdmissionTooSlow(e26Percentile(disabledDurations, 99), e26Percentile(enabledDurations, 99)); slow {
		t.Fatalf("E27 enabled-unused admission far slower than baseline: enabled p99=%s limit=%s", e26Percentile(enabledDurations, 99), limit)
	}
	if _, err := os.Lstat(e27ProviderRoot(enabledState)); !os.IsNotExist(err) {
		t.Fatalf("ordinary starts materialized provider-private state: err=%v", err)
	}
}

func TestE27NoTaxAdmissionProbeStaysLiveAcrossTimedStart(t *testing.T) {
	req := e27NoTaxAdmissionProbe("e27-probe-shape", "/tmp")
	if len(req.Argv) != 1 || req.Argv[0] != "/bin/cat" || req.StdinMode != operation.StdinModeStream || req.YieldMS != 0 {
		t.Fatalf("probe request=%#v", req)
	}
}

func e27NoTaxAdmissionProbe(operationID, cwd string) ipcadapter.RequestV2 {
	return e27Request(ipcadapter.RequestV2{
		Action: "start", OperationID: operationID, CWD: cwd, Argv: []string{"/bin/cat"},
		StdinMode: operation.StdinModeStream, YieldMS: 0, MaxOutputBytes: 1024,
	})
}

func measureE27NativeAdmission(t *testing.T, client *ipcadapter.Client, operationID, cwd string) time.Duration {
	t.Helper()
	startedAt := time.Now()
	response, err := client.CallV2(context.Background(), e27NoTaxAdmissionProbe(operationID, cwd))
	elapsed := time.Since(startedAt)
	if err != nil || !response.OK || response.Result == nil || response.Result.Operation.SessionID == "" {
		t.Fatalf("ordinary start %s response=%#v err=%v", operationID, response, err)
	}
	if response.Result.Operation.State == "terminal" {
		t.Fatalf("ordinary start %s terminalized inside timed admission: %#v", operationID, response.Result.Operation)
	}
	sessionID := response.Result.Operation.SessionID
	eof, err := client.CallV2(context.Background(), e27Request(ipcadapter.RequestV2{
		Action: "write", SessionID: sessionID, InputOffset: 0, EOF: true,
	}))
	if err != nil || !eof.OK {
		t.Fatalf("ordinary cleanup %s response=%#v err=%v", operationID, eof, err)
	}
	waitB1NativeTerminal(t, client, sessionID)
	return elapsed
}

func assertE27NativeRequiredAndUnsupported(t *testing.T, daemon *b1NativeDaemon, cwd, stateDir string) {
	t.Helper()
	marker := filepath.Join(cwd, "required-should-not-run")
	startedAt := time.Now()
	required, err := daemon.client.CallV2(context.Background(), e27Request(ipcadapter.RequestV2{Action: "start", OperationID: "e27-required-reject", CWD: cwd, Argv: []string{"/usr/bin/touch", marker}, TraceMode: trace.ModeRequired}))
	elapsed := time.Since(startedAt)
	if err != nil || required.OK || required.Error == nil || required.Error.Code != string(failure.InputTraceRequiredUnavailable) {
		t.Fatalf("required response=%#v err=%v", required, err)
	}
	if elapsed >= trace.TraceStartupBudget {
		t.Fatalf("required rejection took %s, budget=%s", elapsed, trace.TraceStartupBudget)
	}
	if _, err := os.Lstat(marker); !os.IsNotExist(err) {
		t.Fatalf("required request spawned child: marker err=%v", err)
	}
	for _, request := range []ipcadapter.RequestV2{
		{Action: "start", OperationID: "e27-tty-reject", CWD: cwd, Argv: []string{"/usr/bin/true"}, TTY: true, TraceMode: trace.ModeBestEffort},
		{Action: "start", OperationID: "e27-persistent-reject", CWD: cwd, Argv: []string{"/usr/bin/true"}, Persistent: true, SessionName: "e27-persistent", TraceMode: trace.ModeBestEffort},
	} {
		response, callErr := daemon.client.CallV2(context.Background(), e27Request(request))
		if callErr != nil || response.OK || response.Error == nil || response.Error.Code != string(failure.InputTraceUnsupported) {
			t.Fatalf("unsupported traced request=%#v response=%#v err=%v", request, response, callErr)
		}
	}
	off := callB1NativeDaemon(t, daemon, ipcadapter.RequestV2{Action: "start", OperationID: "e27-tty-off", CWD: cwd, Argv: []string{"/usr/bin/true"}, TTY: true})
	if off.Result == nil {
		t.Fatal("off TTY start missing result")
	}
	waitB1NativeTerminal(t, daemon.client, off.Result.Operation.SessionID)
	if _, err := os.Lstat(e27ProviderRoot(stateDir)); !os.IsNotExist(err) {
		t.Fatalf("required/unsupported/off requests materialized provider-private state: err=%v", err)
	}
}

func assertE27NativeBestEffortTrace(t *testing.T, daemon *b1NativeDaemon, stateDir, fixture, fixtureRoot string, secrets map[string]string) {
	t.Helper()
	readPath := filepath.Join(fixtureRoot, "read.txt")
	metaPath := filepath.Join(fixtureRoot, "meta.txt")
	for path, value := range map[string]string{readPath: "read", metaPath: "meta"} {
		if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	req := ipcadapter.RequestV2{Action: "start", OperationID: "e27-native-best-effort", CWD: fixtureRoot, Argv: []string{fixture, fixtureRoot, readPath, metaPath}, TraceMode: trace.ModeBestEffort, MaxOutputBytes: 4096}
	startedAt := time.Now()
	started := callB1NativeDaemon(t, daemon, req)
	if started.Result == nil || started.InputTrace == nil || started.InputTrace.Record != nil || started.InputTrace.TraceID == "" {
		t.Fatalf("best-effort start leaked/missed concise trace status: %#v", started)
	}
	terminal := waitB1NativeTerminal(t, daemon.client, started.Result.Operation.SessionID)
	if terminal.Child == nil || terminal.Child.Outcome != "success" {
		t.Fatalf("traced child outcome=%#v", terminal.Child)
	}
	inspection := waitE27NativeTraceTerminal(t, daemon.client, req.OperationID)
	t.Logf("E27 first traced start+materialize=%s resources=%d/%d", time.Since(startedAt), inspection.ResourcesReturned, inspection.ResourcesAvailable)
	assertE27TraceRecord(t, inspection)

	// Replay must use the frozen durable binding and never create a new collector.
	replay := callB1NativeDaemon(t, daemon, req)
	if replay.Result == nil || replay.Result.Operation.SessionID != started.Result.Operation.SessionID || replay.InputTrace == nil || replay.InputTrace.TraceID != inspection.TraceID {
		t.Fatalf("trace replay changed authority: start=%#v replay=%#v inspect=%#v", started, replay, inspection)
	}

	latencies := make([]time.Duration, 0, 8)
	for i := 0; i < 8; i++ {
		opID := fmt.Sprintf("e27-native-traced-%d", i)
		request := req
		request.OperationID = opID
		begin := time.Now()
		response := callB1NativeDaemon(t, daemon, request)
		waitB1NativeTerminal(t, daemon.client, response.Result.Operation.SessionID)
		waitE27NativeTraceTerminal(t, daemon.client, opID)
		latencies = append(latencies, time.Since(begin))
	}
	t.Logf("E27 traced start+materialize p50=%s p95=%s p99=%s", e26Percentile(latencies, 50), e26Percentile(latencies, 95), e26Percentile(latencies, 99))

	encoded, err := json.Marshal(inspection)
	if err != nil {
		t.Fatal(err)
	}
	assertE27PrivateValuesAbsent(t, encoded, secrets)
	assertE27PublicStatePrivateValuesAbsent(t, stateDir, daemon.logPath, secrets)
	waitE27TracePrivateCleanup(t, stateDir)
}

func assertE27TraceRecord(t *testing.T, inspection traceapp.InspectResult) {
	t.Helper()
	if inspection.Status != traceapp.InspectTerminal || inspection.Record == nil {
		t.Fatalf("trace inspection=%#v", inspection)
	}
	record := inspection.Record
	if record.Authority != trace.AuthorityAdvisory || record.ScopeKind != trace.ScopeObservedInput || !record.MayHaveUnobservedDependencies || record.Outcome != trace.OutcomePartial || record.PreExecCoverageEstablished {
		t.Fatalf("trace authority/outcome=%#v", record)
	}
	coverage := record.Coverage
	for _, value := range []trace.Coverage{coverage.FilesystemReads, coverage.FilesystemMetadataQueries, coverage.DirectoryEnumerations, coverage.FilesystemWrites, coverage.ExecutedBinaries, coverage.LoadedLibraries, coverage.ChildProcesses} {
		if value != trace.CoveragePartial {
			t.Fatalf("trace overclaimed coverage=%q record=%#v", value, record)
		}
	}
	if coverage.EnvironmentNamesObserved != trace.CoverageUnsupported || coverage.NetworkAttempts != trace.CoverageUnsupported {
		t.Fatalf("unsupported classes overclaimed=%#v", coverage)
	}
	classes := map[trace.ObservationClass]bool{}
	for _, resource := range record.Resources {
		classes[resource.ObservationClass] = true
		if resource.PathClass == trace.PathWorkspaceExternalRedacted && !strings.HasPrefix(resource.Identity, "external-") {
			t.Fatalf("external resource was not redacted: %#v", resource)
		}
	}
	for _, class := range []trace.ObservationClass{trace.ClassFilesystemReads, trace.ClassFilesystemMetadataQueries, trace.ClassDirectoryEnumerations, trace.ClassFilesystemWrites, trace.ClassExecutedBinaries, trace.ClassLoadedLibraries} {
		if !classes[class] {
			t.Fatalf("trace missing representative class %q resources=%#v", class, record.Resources)
		}
	}
}

func waitE27NativeTraceTerminal(t *testing.T, client *ipcadapter.Client, operationID string) traceapp.InspectResult {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		response := callB1Native(t, client, ipcadapter.RequestV2{Action: "inspect.trace", OperationID: operationID, MaxResources: trace.MaxPublicResources})
		if response.InputTrace != nil && response.InputTrace.Status == traceapp.InspectTerminal {
			return *response.InputTrace
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("trace %s did not materialize", operationID)
	return traceapp.InspectResult{}
}

func waitE27TracePrivateCleanup(t *testing.T, stateDir string) {
	t.Helper()
	root := filepath.Join(e27ProviderRoot(stateDir), "traces")
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		entries, err := os.ReadDir(root)
		if err == nil && len(entries) == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	entries, err := os.ReadDir(root)
	t.Fatalf("trace private state not cleaned entries=%v err=%v", entries, err)
}

func e27ProviderRoot(stateDir string) string {
	return filepath.Join(stateDir, "input-trace", "dyld-v1")
}

func assertE27PrivateValuesAbsent(t *testing.T, encoded []byte, secrets map[string]string) {
	t.Helper()
	text := string(encoded)
	for key, value := range secrets {
		for _, forbidden := range []string{key, value} {
			if strings.Contains(text, forbidden) {
				t.Fatalf("public trace leaked %q: %s", forbidden, text)
			}
		}
	}
	for _, forbidden := range []string{"SHELLBEAM_TRACE_SOCKET", "DYLD_INSERT_LIBRARIES", "raw.events", "socket_path", "dylib_path", `"raw_events":`, "proven_input_scope"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("public trace leaked private field/value %q: %s", forbidden, text)
		}
	}
}

func assertE27PublicStatePrivateValuesAbsent(t *testing.T, stateDir, logPath string, secrets map[string]string) {
	t.Helper()
	privateRoot := e27ProviderRoot(stateDir)
	var public []byte
	err := filepath.Walk(stateDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if path == privateRoot {
			return filepath.SkipDir
		}
		if info.Mode().IsRegular() {
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			public = append(public, data...)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(logPath); err == nil {
		public = append(public, data...)
	}
	assertE27PrivateValuesAbsent(t, public, secrets)
	for _, forbidden := range []string{"/tmp/shellbeam-e27-", filepath.Join("input-trace", "dyld-v1", "artifacts")} {
		if strings.Contains(string(public), forbidden) {
			t.Fatalf("public state leaked provider-private path %q", forbidden)
		}
	}
}

func buildE27AcceptanceFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	source := filepath.Join(root, "fixture.c")
	binary := filepath.Join(root, "fixture")
	code := `
#include <arpa/inet.h>
#include <dirent.h>
#include <dlfcn.h>
#include <fcntl.h>
#include <netinet/in.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/socket.h>
#include <sys/stat.h>
#include <sys/types.h>
#include <sys/wait.h>
#include <unistd.h>
int main(int argc, char **argv, char **envp) {
  if (argc != 4) return 20;
  int r=open(argv[2],O_RDONLY); if(r<0)return 21; char b[8]; (void)read(r,b,sizeof(b)); close(r);
  struct stat st; if(stat(argv[3],&st)!=0)return 22;
  DIR *d=opendir(argv[1]); if(!d)return 23; (void)readdir(d); closedir(d);
  char out[4096]; snprintf(out,sizeof(out),"%s/write.txt",argv[1]); int w=open(out,O_WRONLY|O_CREAT|O_TRUNC,0600); if(w<0)return 24; (void)write(w,"x",1); close(w);
  const char *external=getenv("E27_PRIVATE_EXTERNAL"); if(!external)return 25; int x=open(external,O_RDONLY); if(x<0)return 26; close(x);
  void *h=dlopen("/usr/lib/libSystem.B.dylib",RTLD_LAZY); if(h)dlclose(h);
  int s=socket(AF_INET,SOCK_DGRAM,0); if(s>=0){struct sockaddr_in a; memset(&a,0,sizeof(a)); a.sin_family=AF_INET; a.sin_port=htons(9); a.sin_addr.s_addr=htonl(INADDR_LOOPBACK); const char *p=getenv("E27_NETWORK_PAYLOAD"); if(p)(void)sendto(s,p,strlen(p),0,(struct sockaddr*)&a,sizeof(a)); close(s);}
  pid_t p=fork(); if(p==0){char *a[]={(char*)"/usr/bin/true",NULL}; execve(a[0],a,envp); _exit(27);} if(p<0)return 28; int status=0; waitpid(p,&status,0);
  return 0;
}`
	if err := os.WriteFile(source, []byte(code), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("/usr/bin/clang", source, "-o", binary)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("compile E27 acceptance fixture: %v\n%s", err, output)
	}
	return binary
}

func e27Request(req ipcadapter.RequestV2) ipcadapter.RequestV2 {
	req.IPVersion, req.Kind = 2, "request"
	if req.RequestID == "" {
		req.RequestID = fmt.Sprintf("e27-%d", time.Now().UnixNano())
	}
	return req
}
