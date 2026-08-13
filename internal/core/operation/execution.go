package operation

type ExecutionSpec struct {
	Shell     string
	Command   string
	CWD       string
	TTY       bool
	TimeoutMS int64
}
