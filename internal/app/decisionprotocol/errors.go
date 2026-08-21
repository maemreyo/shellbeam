package decisionprotocol

import "errors"

var (
	ErrEpisodeNotFound    = errors.New("decision episode not found")
	ErrCandidateNotFound  = errors.New("decision candidate not found")
	ErrExperimentNotFound = errors.New("decision experiment not found")
)
