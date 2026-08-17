package operation

type EnvironmentEntry struct {
	Key   string
	Value string
}

type ExecutionSpec struct {
	Mode                 ExecutionMode
	Shell                string
	Executable           string
	BindingErrorCode     string
	Command              string
	Argv                 []string
	CWD                  string
	TTY                  bool
	TimeoutMS            int64
	EnvironmentAdditions []EnvironmentEntry
	// StdinMode is resolved policy, never the caller's raw request: by the time
	// a spec exists the choice has been made, so the spawner does not have to
	// know what a default is.
	StdinMode StdinMode
}
