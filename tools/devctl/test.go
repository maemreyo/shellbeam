package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
)

func command(name string, args ...string) ([]byte, error) {
	c := exec.Command(name, args...)
	c.Stderr = os.Stderr
	return c.Output()
}

func listPackages() ([]string, error) {
	b, err := command("go", "list", "./...")
	if err != nil {
		return nil, err
	}
	p := strings.Fields(string(b))
	sort.Strings(p)
	return p, nil
}

func changedFiles(base string) ([]string, error) {
	var all []string
	for _, args := range [][]string{{"diff", "--name-only", base + "...HEAD"}, {"diff", "--name-only"}, {"diff", "--name-only", "--cached"}, {"ls-files", "--others", "--exclude-standard"}} {
		b, err := command("git", args...)
		if err != nil {
			return nil, err
		}
		all = append(all, strings.Fields(string(b))...)
	}
	seen := map[string]bool{}
	out := all[:0]
	for _, p := range all {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out, nil
}

func runGoTest(packages []string, race bool) error {
	if len(packages) == 0 {
		return fmt.Errorf("empty package selection")
	}
	args := []string{"test"}
	if race {
		args = append(args, "-race")
	}
	args = append(args, packages...)
	c := exec.Command("go", args...)
	var out bytes.Buffer
	c.Stdout = &out
	c.Stderr = &out
	if err := c.Run(); err != nil {
		return fmt.Errorf("go test: %w\n%s", err, out.String())
	}
	fmt.Print(out.String())
	return nil
}

func decodeGoList(data []byte) ([]map[string]any, error) {
	d := json.NewDecoder(bytes.NewReader(data))
	var out []map[string]any
	for d.More() {
		var v map[string]any
		if err := d.Decode(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}
