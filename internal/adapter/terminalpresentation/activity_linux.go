//go:build linux

package terminalpresentation

import (
	"context"
	"errors"

	app "github.com/maemreyo/shellbeam/internal/app/terminalpresentation"
)

type LinuxActivitySource struct{}

func NewLinuxActivitySource() *LinuxActivitySource { return &LinuxActivitySource{} }

func (*LinuxActivitySource) Current(context.Context) (app.ForegroundObservation, error) {
	return app.ForegroundObservation{}, errors.New("Linux terminal activity source is unqualified")
}

func (*LinuxActivitySource) Run(context.Context, func(app.ForegroundObservation) error) error {
	return errors.New("Linux terminal activity source is unqualified")
}
