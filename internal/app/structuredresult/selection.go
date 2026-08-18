package structuredresult

import "github.com/maemreyo/shellbeam/internal/core/operation"

type SelectionStatus string

const (
	SelectionNone        SelectionStatus = "none"
	SelectionSelected    SelectionStatus = "selected"
	SelectionUnsupported SelectionStatus = "unsupported"
)

type AdapterSelection struct {
	Status          SelectionStatus
	AdapterID       string
	Source          string
	ObservationCode string
}

func SelectAdapter(explicit string, argv []string) AdapterSelection {
	if explicit != "" {
		if !operation.ValidStructuredAdapterID(explicit) {
			return AdapterSelection{Status: SelectionUnsupported, AdapterID: explicit, Source: "explicit", ObservationCode: "structured_adapter_unsupported"}
		}
		if supportedAdapter(explicit) {
			return AdapterSelection{Status: SelectionSelected, AdapterID: explicit, Source: "explicit"}
		}
		return AdapterSelection{Status: SelectionUnsupported, AdapterID: explicit, Source: "explicit", ObservationCode: "structured_adapter_unsupported"}
	}
	if len(argv) >= 3 && argv[0] == "go" && argv[2] == "-json" {
		switch argv[1] {
		case "test":
			return AdapterSelection{Status: SelectionSelected, AdapterID: "go-test-json", Source: "direct_argv"}
		case "vet":
			return AdapterSelection{Status: SelectionSelected, AdapterID: "go-vet-json", Source: "direct_argv"}
		}
	}
	return AdapterSelection{Status: SelectionNone}
}

func supportedAdapter(id string) bool { return id == "go-test-json" || id == "go-vet-json" }

// AdapterAcceptsArgv reports whether direct argv is known to emit the native
// machine-readable format consumed by adapter. It is intentionally strict:
// shell strings and wrappers are not parsed or guessed here.
func AdapterAcceptsArgv(adapter string, argv []string) bool {
	if len(argv) < 3 || argv[0] != "go" || !hasJSONOutputFlag(argv[2:]) {
		return false
	}
	switch adapter {
	case "go-test-json":
		return argv[1] == "test"
	case "go-vet-json":
		return argv[1] == "vet"
	default:
		return false
	}
}

func hasJSONOutputFlag(args []string) bool {
	for _, arg := range args {
		if arg == "-json" || arg == "-json=true" {
			return true
		}
	}
	return false
}
