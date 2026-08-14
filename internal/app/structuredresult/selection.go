package structuredresult

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
