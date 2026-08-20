//go:build darwin || linux

package localfs

import (
	"context"
	"sync"
	"testing"
)

func TestArtifactPathAuthorityConcurrentCloseIsIdempotent(t *testing.T) {
	root := t.TempDir()
	authority, _, err := QualifyArtifactAbsentBaseline(context.Background(), root, "junit.xml")
	if err != nil {
		t.Fatal(err)
	}
	const callers = 32
	var wg sync.WaitGroup
	errs := make(chan error, callers)
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- authority.Close()
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("close: %v", err)
		}
	}
}

func TestArtifactAbsentBaselineConcurrentIndependentPaths(t *testing.T) {
	root := t.TempDir()
	const paths = 16
	var wg sync.WaitGroup
	errs := make(chan error, paths)
	for i := 0; i < paths; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			path := "junit-" + string(rune('a'+i)) + ".xml"
			authority, _, err := QualifyArtifactAbsentBaseline(context.Background(), root, path)
			if err == nil {
				err = authority.Close()
			}
			errs <- err
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("baseline: %v", err)
		}
	}
}
