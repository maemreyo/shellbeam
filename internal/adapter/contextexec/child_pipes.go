package contextexec

import (
	"errors"
	"os"
	"os/exec"
)

type childOutputPipes struct {
	stdoutR *os.File
	stdoutW *os.File
	stderrR *os.File
	stderrW *os.File
}

func attachChildOutputPipes(cmd *exec.Cmd) (*childOutputPipes, error) {
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		_ = stdoutR.Close()
		_ = stdoutW.Close()
		return nil, err
	}
	pipes := &childOutputPipes{stdoutR: stdoutR, stdoutW: stdoutW, stderrR: stderrR, stderrW: stderrW}
	cmd.Stdout = stdoutW
	cmd.Stderr = stderrW
	return pipes, nil
}

func (p *childOutputPipes) closeParentWriters() error {
	if p == nil {
		return nil
	}
	var err error
	if p.stdoutW != nil {
		err = errors.Join(err, p.stdoutW.Close())
		p.stdoutW = nil
	}
	if p.stderrW != nil {
		err = errors.Join(err, p.stderrW.Close())
		p.stderrW = nil
	}
	return err
}

func (p *childOutputPipes) closeAll() error {
	if p == nil {
		return nil
	}
	err := p.closeParentWriters()
	if p.stdoutR != nil {
		err = errors.Join(err, p.stdoutR.Close())
		p.stdoutR = nil
	}
	if p.stderrR != nil {
		err = errors.Join(err, p.stderrR.Close())
		p.stderrR = nil
	}
	return err
}
