package process

import (
	"bytes"
	"context"
	"errors"
	"net"
	"os/exec"
	"sort"
	"strconv"
	"strings"

	"github.com/maemreyo/shellbeam/internal/core/failure"
	core "github.com/maemreyo/shellbeam/internal/core/process"
)

const maxPortProbeOutputBytes = 32 << 10

type PortRunner interface {
	Run(context.Context, []string, int) ([]byte, error)
}

type PortInspector struct {
	runner PortRunner
}

func NewPortInspector(runner PortRunner) *PortInspector {
	if runner == nil {
		runner = hostPortRunner{}
	}
	return &PortInspector{runner: runner}
}

func NewHostPortInspector() *PortInspector {
	return NewPortInspector(hostPortRunner{})
}

func (p *PortInspector) Observe(ctx context.Context, pids []int) ([]core.PortObservation, error) {
	normalized := normalizePortPIDs(pids)
	if len(normalized) == 0 {
		return nil, nil
	}
	parts := make([]string, 0, len(normalized))
	for _, pid := range normalized {
		parts = append(parts, strconv.Itoa(pid))
	}
	argv := []string{"lsof", "-nP", "-a", "-p", strings.Join(parts, ","), "-iTCP", "-sTCP:LISTEN", "-F", "pn"}
	output, err := p.runner.Run(ctx, argv, maxPortProbeOutputBytes)
	if err != nil {
		return nil, failure.New(failure.PortObservationUnavailable, map[string]string{"reason": "lsof_unavailable"}, err)
	}
	return parseLsofPorts(output), nil
}

func normalizePortPIDs(values []int) []int {
	seen := make(map[int]struct{}, len(values))
	out := make([]int, 0, len(values))
	for _, pid := range values {
		if pid <= 0 {
			continue
		}
		if _, ok := seen[pid]; ok {
			continue
		}
		seen[pid] = struct{}{}
		out = append(out, pid)
	}
	sort.Ints(out)
	return out
}

func parseLsofPorts(output []byte) []core.PortObservation {
	currentPID := 0
	seen := map[string]struct{}{}
	ports := make([]core.PortObservation, 0)
	for _, raw := range strings.Split(string(output), "\n") {
		if raw == "" {
			continue
		}
		switch raw[0] {
		case 'p':
			pid, err := strconv.Atoi(strings.TrimSpace(raw[1:]))
			if err != nil || pid <= 0 {
				currentPID = 0
				continue
			}
			currentPID = pid
		case 'n':
			if currentPID <= 0 {
				continue
			}
			endpoint := strings.TrimSpace(raw[1:])
			if cut := strings.Index(endpoint, "->"); cut >= 0 {
				endpoint = endpoint[:cut]
			}
			host, port, ok := splitLocalEndpoint(endpoint)
			if !ok {
				continue
			}
			class := classifyLocalEndpoint(host)
			key := strconv.Itoa(currentPID) + "|" + strconv.Itoa(port) + "|" + class
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			ports = append(ports, core.PortObservation{
				PID:                currentPID,
				Protocol:           "tcp",
				LocalEndpointClass: class,
				Port:               port,
				Quality:            core.PortComplete,
			})
		}
	}
	sort.Slice(ports, func(i, j int) bool {
		if ports[i].PID != ports[j].PID {
			return ports[i].PID < ports[j].PID
		}
		if ports[i].Port != ports[j].Port {
			return ports[i].Port < ports[j].Port
		}
		return ports[i].LocalEndpointClass < ports[j].LocalEndpointClass
	})
	if len(ports) > core.MaxPortRecords {
		ports = ports[:core.MaxPortRecords]
	}
	return ports
}

func splitLocalEndpoint(endpoint string) (string, int, bool) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return "", 0, false
	}
	host, portText, err := net.SplitHostPort(endpoint)
	if err != nil {
		idx := strings.LastIndex(endpoint, ":")
		if idx <= 0 || idx == len(endpoint)-1 {
			return "", 0, false
		}
		host, portText = endpoint[:idx], endpoint[idx+1:]
	}
	host = strings.Trim(strings.TrimSpace(host), "[]")
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return "", 0, false
	}
	return host, port, true
}

func classifyLocalEndpoint(host string) string {
	host = strings.TrimSpace(host)
	if host == "" || host == "*" || host == "0.0.0.0" || host == "::" {
		return "wildcard"
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return "loopback"
	}
	if strings.EqualFold(host, "localhost") {
		return "loopback"
	}
	return "local"
}

type hostPortRunner struct{}

func (hostPortRunner) Run(ctx context.Context, argv []string, maxBytes int) ([]byte, error) {
	if len(argv) == 0 || maxBytes < 1 {
		return nil, exec.ErrNotFound
	}
	path, err := exec.LookPath(argv[0])
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, path, argv[1:]...)
	buffer := &boundedPortBuffer{limit: maxBytes}
	cmd.Stdout = buffer
	cmd.Stderr = buffer
	if err := cmd.Run(); err != nil {
		// lsof exits 1 when the bounded selector has no matching listening
		// sockets. Empty output is therefore a complete empty observation, not
		// provider unavailability. Non-empty diagnostics and other exit codes
		// remain fail-closed.
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 && len(buffer.Bytes()) == 0 {
			return nil, nil
		}
		return nil, err
	}
	return buffer.Bytes(), nil
}

type boundedPortBuffer struct {
	limit int
	data  bytes.Buffer
}

func (b *boundedPortBuffer) Write(p []byte) (int, error) {
	remaining := b.limit - b.data.Len()
	if remaining > 0 {
		if remaining > len(p) {
			remaining = len(p)
		}
		_, _ = b.data.Write(p[:remaining])
	}
	return len(p), nil
}

func (b *boundedPortBuffer) Bytes() []byte {
	return append([]byte(nil), b.data.Bytes()...)
}
