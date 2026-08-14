package operation

type ExecutionSpec struct {
	Mode             ExecutionMode
	Shell            string
	Executable       string
	BindingErrorCode string
	Command          string
	Argv             []string
	CWD              string
	TTY              bool
	TimeoutMS        int64
}
