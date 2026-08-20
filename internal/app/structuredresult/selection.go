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

func supportedAdapter(id string) bool {
	return id == "go-test-json" || id == "go-vet-json" || id == PytestJUnitAdapterID || id == JestJSONAdapterID
}

func SelectAdapterWithCapture(explicit string, argv []string, binding *ProducerInvocationBinding) AdapterSelection {
	if explicit == PytestJUnitAdapterID || explicit == JestJSONAdapterID {
		if binding != nil && binding.Validate() == nil && binding.AdapterID() == explicit && AdapterAcceptsArgv(explicit, argv) {
			return AdapterSelection{Status: SelectionSelected, AdapterID: explicit, Source: "explicit"}
		}
		return AdapterSelection{Status: SelectionUnsupported, AdapterID: explicit, Source: "explicit", ObservationCode: "structured_adapter_precondition_failed"}
	}
	if explicit != "" {
		return SelectAdapter(explicit, argv)
	}
	if binding != nil && binding.Validate() == nil {
		adapterID := binding.AdapterID()
		if AdapterAcceptsArgv(adapterID, argv) {
			source := "qualified_producer_invocation"
			switch binding.Kind {
			case ProducerInvocationPytest:
				source = "qualified_pytest_invocation"
			case ProducerInvocationJest:
				source = "qualified_jest_invocation"
			}
			return AdapterSelection{Status: SelectionSelected, AdapterID: adapterID, Source: source}
		}
	}
	return SelectAdapter("", argv)
}

func SelectAdapterWithPytest(explicit string, argv []string, binding *PytestInvocationBindingV1) AdapterSelection {
	var producer *ProducerInvocationBinding
	if binding != nil {
		producer = &ProducerInvocationBinding{Kind: ProducerInvocationPytest, PytestInvocation: binding}
	}
	return SelectAdapterWithCapture(explicit, argv, producer)
}

func PytestCandidateArgv(argv []string) bool {
	_, args, ok := pytestProducer(argv)
	if !ok {
		return false
	}
	for _, arg := range args {
		if len(arg) > 0 && arg[0] == '@' {
			return false
		}
	}
	resolved, ok := resolvePytestAuthorityArgs(args)
	return ok && resolved.junitPath != "" && resolved.junitFamily == "junit_family=xunit2" && resolved.addopts == "addopts="
}

// AdapterAcceptsArgv reports whether direct argv is known to emit the native
// machine-readable format consumed by adapter. It is intentionally strict:
// shell strings and wrappers are not parsed or guessed here.
func AdapterAcceptsArgv(adapter string, argv []string) bool {
	switch adapter {
	case PytestJUnitAdapterID:
		return PytestCandidateArgv(argv)
	case JestJSONAdapterID:
		return JestCandidateArgv(argv)
	}
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
