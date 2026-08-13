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
	return commandIn("", name, args...)
}

func commandIn(dir, name string, args ...string) ([]byte, error) {
	c := exec.Command(name, args...)
	c.Dir = dir
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
	return changedFilesIn(".", base)
}

func changedFilesIn(root, base string) ([]string, error) {
	seen := map[string]bool{}
	for _, args := range [][]string{
		{"diff", "--name-status", "-z", base + "...HEAD"},
		{"diff", "--name-status", "-z"},
		{"diff", "--name-status", "-z", "--cached"},
	} {
		b, err := commandIn(root, "git", args...)
		if err != nil {
			return nil, err
		}
		paths, err := parseNameStatusZ(b)
		if err != nil {
			return nil, err
		}
		for _, p := range paths {
			seen[p] = true
		}
	}
	b, err := commandIn(root, "git", "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, err
	}
	for _, p := range splitNUL(b) {
		seen[p] = true
	}
	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Strings(out)
	return out, nil
}

func parseNameStatusZ(data []byte) ([]string, error) {
	fields := splitNUL(data)
	var paths []string
	for i := 0; i < len(fields); {
		status := fields[i]
		i++
		if status == "" || i >= len(fields) {
			return nil, fmt.Errorf("malformed git name-status output")
		}
		paths = append(paths, fields[i])
		i++
		if status[0] == 'R' || status[0] == 'C' {
			if i >= len(fields) {
				return nil, fmt.Errorf("malformed git rename/copy output")
			}
			paths = append(paths, fields[i])
			i++
		}
	}
	return paths, nil
}

func splitNUL(data []byte) []string {
	parts := strings.Split(string(data), "\x00")
	if len(parts) != 0 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	return parts
}

func runGoTest(packages []string, race bool) error {
	if len(packages) == 0 {
		return nil
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
