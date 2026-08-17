//go:build darwin

package dyld

import (
	"encoding/binary"
	"errors"
	"net"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	traceapp "github.com/maemreyo/shellbeam/internal/app/inputtrace"
	trace "github.com/maemreyo/shellbeam/internal/core/inputtrace"
)

type collectorLimits struct {
	maxEvents       int
	maxUnique       int
	maxPrivateBytes int64
	maxDuration     time.Duration
}

func defaultCollectorLimits() collectorLimits {
	return collectorLimits{maxEvents: trace.MaxRawEvents, maxUnique: trace.MaxUniqueResources, maxPrivateBytes: trace.MaxPrivateRawBytes, maxDuration: trace.MaxTraceCaptureDuration}
}

type collector struct {
	mu           sync.Mutex
	conn         *net.UnixConn
	raw          *os.File
	socketPath   string
	rawPath      string
	traceID      string
	startedAt    time.Time
	limits       collectorLimits
	closed       bool
	truncated    bool
	rawEvents    int
	malformed    int
	privateBytes int64
	resources    map[string]traceapp.ProviderResource
	wg           sync.WaitGroup
	finalizeMu   sync.Mutex
}

func newCollector(traceDir, socketDir, traceID string, limits collectorLimits) (*collector, error) {
	if limits.maxEvents < 1 || limits.maxUnique < 1 || limits.maxPrivateBytes < 1 || limits.maxDuration <= 0 {
		return nil, errors.New("invalid collector limits")
	}
	rawPath := traceDir + string(os.PathSeparator) + "raw.events"
	raw, err := os.OpenFile(rawPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	if err := raw.Chmod(0600); err != nil {
		raw.Close()
		return nil, err
	}
	socketPath := filepath.Join(socketDir, traceID+".sock")
	conn, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: socketPath, Net: "unixgram"})
	if err != nil {
		raw.Close()
		os.Remove(rawPath)
		return nil, err
	}
	if err := os.Chmod(socketPath, 0600); err != nil {
		conn.Close()
		raw.Close()
		os.Remove(socketPath)
		os.Remove(rawPath)
		return nil, err
	}
	c := &collector{conn: conn, raw: raw, socketPath: socketPath, rawPath: rawPath, traceID: traceID, startedAt: time.Now().UTC(), limits: limits, resources: map[string]traceapp.ProviderResource{}}
	c.wg.Add(1)
	go c.readLoop()
	return c, nil
}

func (c *collector) readLoop() {
	defer c.wg.Done()
	buf := make([]byte, maxWireEventBytes+1)
	for {
		n, _, err := c.conn.ReadFromUnix(buf)
		if err != nil {
			return
		}
		if n == 1 && buf[0] == 0 {
			return
		}
		c.ingest(append([]byte(nil), buf[:n]...))
	}
}

func (c *collector) ingest(raw []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	if time.Since(c.startedAt) > c.limits.maxDuration {
		c.truncated = true
		return
	}
	event, err := decodeEvent(raw)
	if err != nil {
		c.malformed++
		return
	}
	if c.rawEvents >= c.limits.maxEvents {
		c.truncated = true
		return
	}
	class, ok := observationClass(event.class)
	if !ok {
		c.malformed++
		return
	}
	key := string(class) + "\x00" + event.path
	if _, exists := c.resources[key]; !exists && len(c.resources) >= c.limits.maxUnique {
		c.truncated = true
		return
	}
	needed := int64(4 + len(raw))
	if c.privateBytes+needed > c.limits.maxPrivateBytes {
		c.truncated = true
		return
	}
	var prefix [4]byte
	binary.LittleEndian.PutUint32(prefix[:], uint32(len(raw)))
	if _, err := c.raw.Write(prefix[:]); err != nil {
		c.truncated = true
		return
	}
	if _, err := c.raw.Write(raw); err != nil {
		c.truncated = true
		return
	}
	c.privateBytes += needed
	c.rawEvents++
	c.resources[key] = traceapp.ProviderResource{ObservationClass: class, Path: event.path}
}

func (c *collector) snapshotLocked(end time.Time) traceapp.ProviderSnapshot {
	resources := make([]traceapp.ProviderResource, 0, len(c.resources))
	for _, resource := range c.resources {
		resources = append(resources, resource)
	}
	sort.Slice(resources, func(i, j int) bool {
		if resources[i].ObservationClass != resources[j].ObservationClass {
			return resources[i].ObservationClass < resources[j].ObservationClass
		}
		return resources[i].Path < resources[j].Path
	})
	return traceapp.ProviderSnapshot{TraceID: c.traceID, CaptureStart: c.startedAt, CaptureEnd: end, Coverage: providerCoverage(), Truncated: c.truncated, Resources: resources, RawEventCount: c.rawEvents}
}

func (c *collector) finalize() traceapp.ProviderSnapshot {
	c.finalizeMu.Lock()
	defer c.finalizeMu.Unlock()
	c.mu.Lock()
	if c.closed {
		snapshot := c.snapshotLocked(time.Now().UTC())
		c.mu.Unlock()
		return snapshot
	}
	conn := c.conn
	socketPath := c.socketPath
	c.mu.Unlock()
	if err := signalCollectorStop(socketPath); err != nil {
		c.mu.Lock()
		c.truncated = true
		c.mu.Unlock()
		_ = conn.Close()
	}
	c.wg.Wait()
	_ = conn.Close()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	_ = c.raw.Sync()
	_ = c.raw.Close()
	_ = os.Remove(c.socketPath)
	return c.snapshotLocked(time.Now().UTC())
}

func signalCollectorStop(socketPath string) error {
	conn, err := net.DialUnix("unixgram", nil, &net.UnixAddr{Name: socketPath, Net: "unixgram"})
	if err != nil {
		return err
	}
	defer conn.Close()
	deadline := time.Now().Add(100 * time.Millisecond)
	for {
		if _, err = conn.Write([]byte{0}); err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return err
		}
		time.Sleep(time.Millisecond)
	}
}

func (c *collector) abort() {
	_ = c.finalize()
	_ = os.Remove(c.rawPath)
	_ = os.Remove(filepath.Dir(c.rawPath))
}
