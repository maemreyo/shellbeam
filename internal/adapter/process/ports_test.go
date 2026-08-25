package process

import (
	"context"
	"errors"
	"os/exec"
	"reflect"
	"testing"

	"github.com/maemreyo/shellbeam/internal/core/failure"
	core "github.com/maemreyo/shellbeam/internal/core/process"
)

type fakePortRunner struct {
	argv   [][]string
	output []byte
	err    error
}

func (f *fakePortRunner) Run(_ context.Context, argv []string, _ int) ([]byte, error) {
	f.argv = append(f.argv, append([]string(nil), argv...))
	return append([]byte(nil), f.output...), f.err
}

func TestPortInspectorUsesFixedLocalLsofAndNormalizesDeterministically(t *testing.T) {
	runner := &fakePortRunner{output: []byte("p10\nn127.0.0.1:8080\nn*:9090\np11\nn[::1]:7070\nn10.0.0.5:6060\np10\nn127.0.0.1:8080\n")}
	got, err := NewPortInspector(runner).Observe(context.Background(), []int{11, 10, 10})
	if err != nil {
		t.Fatal(err)
	}
	wantArgv := []string{"lsof", "-nP", "-a", "-p", "10,11", "-iTCP", "-sTCP:LISTEN", "-F", "pn"}
	if len(runner.argv) != 1 || !reflect.DeepEqual(runner.argv[0], wantArgv) {
		t.Fatalf("argv=%v", runner.argv)
	}
	want := []core.PortObservation{
		{PID: 10, Protocol: "tcp", LocalEndpointClass: "loopback", Port: 8080, Quality: core.PortComplete},
		{PID: 10, Protocol: "tcp", LocalEndpointClass: "wildcard", Port: 9090, Quality: core.PortComplete},
		{PID: 11, Protocol: "tcp", LocalEndpointClass: "local", Port: 6060, Quality: core.PortComplete},
		{PID: 11, Protocol: "tcp", LocalEndpointClass: "loopback", Port: 7070, Quality: core.PortComplete},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ports=%#v want %#v", got, want)
	}
}

func TestPortInspectorBoundsRecordsAndClassifiesUnavailable(t *testing.T) {
	t.Run("bounded", func(t *testing.T) {
		text := "p10\n"
		for i := 0; i < core.MaxPortRecords+5; i++ {
			text += "n127.0.0.1:" + itoaTest(1000+i) + "\n"
		}
		got, err := NewPortInspector(&fakePortRunner{output: []byte(text)}).Observe(context.Background(), []int{10})
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != core.MaxPortRecords {
			t.Fatalf("ports=%d", len(got))
		}
	})
	t.Run("unavailable", func(t *testing.T) {
		_, err := NewPortInspector(&fakePortRunner{err: errors.New("private lsof failure token=secret")}).Observe(context.Background(), []int{10})
		if !errors.Is(err, failure.PortObservationUnavailable) {
			t.Fatalf("err=%v", err)
		}
	})
}

func TestHostPortRunnerTreatsEmptyExitOneAsNoListeners(t *testing.T) {
	if _, err := exec.LookPath("/bin/sh"); err != nil {
		t.Skip("/bin/sh unavailable")
	}
	got, err := (hostPortRunner{}).Run(context.Background(), []string{"/bin/sh", "-c", "exit 1"}, 1024)
	if err != nil || len(got) != 0 {
		t.Fatalf("empty no-match result=%q err=%v", got, err)
	}

	_, err = (hostPortRunner{}).Run(context.Background(), []string{"/bin/sh", "-c", "printf diagnostic >&2; exit 1"}, 1024)
	if err == nil {
		t.Fatal("non-empty exit-one diagnostic was accepted as no listeners")
	}
}

func TestParseLsofPortsRejectsRemoteOrMalformedFacts(t *testing.T) {
	got := parseLsofPorts([]byte("p10\nn127.0.0.1:8080->1.2.3.4:443\nnmalformed\npnope\nn*:9090\n"))
	if len(got) != 1 || got[0].Port != 8080 || got[0].LocalEndpointClass != "loopback" {
		t.Fatalf("ports=%#v", got)
	}
}

func itoaTest(value int) string {
	if value == 0 {
		return "0"
	}
	buf := [20]byte{}
	i := len(buf)
	for value > 0 {
		i--
		buf[i] = byte('0' + value%10)
		value /= 10
	}
	return string(buf[i:])
}
