package decisionprotocol

import (
	"errors"
	"fmt"
)

type ReasonError struct {
	Reason ReasonCode
	Detail string
}

func (e *ReasonError) Error() string {
	if e == nil {
		return ""
	}
	if e.Detail == "" {
		return string(e.Reason)
	}
	return fmt.Sprintf("%s: %s", e.Reason, e.Detail)
}

func NewReasonError(reason ReasonCode, detail string) error {
	return &ReasonError{Reason: reason, Detail: detail}
}

func ReasonOf(err error) (ReasonCode, bool) {
	var target *ReasonError
	if !errors.As(err, &target) || target == nil {
		return "", false
	}
	return target.Reason, true
}
