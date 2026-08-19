package terminalpresentation

import (
	"context"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/terminalpresentation"
)

type ForegroundObservation struct {
	Identity   *core.TerminalIdentity
	ObservedAt time.Time
	Quality    core.EvidenceQuality
}

func (v ForegroundObservation) Validate() error {
	if v.ObservedAt.IsZero() {
		return errInvalidForegroundObservation
	}
	if err := v.Quality.Validate(); err != nil {
		return err
	}
	if v.Identity != nil {
		return v.Identity.Validate()
	}
	return nil
}

type ActivitySource interface {
	Current(context.Context) (ForegroundObservation, error)
	Run(context.Context, func(ForegroundObservation) error) error
}

type RunningSource interface {
	Running(context.Context) ([]core.TerminalIdentity, error)
}
