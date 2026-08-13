// Package control defines service status and boundary-specific diagnostics.
package control

type Status string

const (
	Pass   Status = "pass"
	Warn   Status = "warn"
	Fail   Status = "fail"
	NotRun Status = "not_run"
)

type Check struct {
	ID      string `json:"id"`
	Status  Status `json:"status"`
	Message string `json:"message"`
	Hint    string `json:"hint,omitempty"`
}
type Report struct {
	SchemaVersion int     `json:"schema_version"`
	Checks        []Check `json:"checks"`
}

func (r Report) ExitCode() int {
	for _, c := range r.Checks {
		if c.Status == Fail {
			return 1
		}
	}
	return 0
}
