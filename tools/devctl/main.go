// Command devctl provides deterministic local quality and test evidence.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

func main() {
	code, err := run(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
	os.Exit(code)
}

func run(args []string) (int, error) {
	if len(args) == 0 {
		return 2, fmt.Errorf("usage: devctl explain|check|test|verify")
	}
	base := argValue(args, "--base", "main")
	receipt := Evidence{SchemaVersion: 1, Command: args[0], Base: base, StartedAt: time.Now().UTC()}
	fingerprint, err := sourceFingerprint(".")
	if err != nil {
		return 1, err
	}
	receipt.SourceFingerprint = fingerprint
	receipt.ChangedFiles, _ = changedFiles(base)
	receipt.SelectedPackages, err = listPackages()
	if err != nil {
		return 1, err
	}
	switch args[0] {
	case "explain":
	case "check":
		err = checkRepository(".")
	case "test":
		err = runGoTest(receipt.SelectedPackages, false)
	case "verify":
		if err = checkRepository("."); err == nil {
			err = runGoTest(receipt.SelectedPackages, false)
		}
	case "release-evidence":
		path := argValue(args, "--out", ".build/release/release-evidence.json")
		if err = writeReleaseEvidence(path, receipt.SourceFingerprint); err == nil {
			fmt.Println(path)
			return 0, nil
		}
	default:
		return 2, fmt.Errorf("unknown devctl command %q", args[0])
	}
	if err != nil {
		receipt.ExitCode = 1
		receipt.Status = "failed"
		receipt.Error = err.Error()
	} else {
		receipt.Status = "passed"
	}
	receipt.FinishedAt = time.Now().UTC()
	path, writeErr := writeEvidence(receipt)
	if writeErr != nil {
		return 1, writeErr
	}
	if hasArg(args, "--json") {
		_ = json.NewEncoder(os.Stdout).Encode(receipt)
	} else {
		fmt.Printf("%s: %s (%s)\n", args[0], receipt.Status, path)
	}
	if err != nil {
		return 1, err
	}
	return 0, nil
}

func argValue(args []string, name, fallback string) string {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == name {
			return args[i+1]
		}
	}
	return fallback
}
func hasArg(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}
