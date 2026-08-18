package main

const (
	candidatePerSessionObserver = "per_session_observer"
	candidateSharedPerPane      = "shared_observer_with_per_pane_off"
	candidateSharedDaemonDemux  = "shared_observer_with_daemon_demux_simulation"
)

func privacyCandidateNames() []string {
	return []string{
		candidatePerSessionObserver,
		candidateSharedPerPane,
		candidateSharedDaemonDemux,
	}
}

type privacyPaneSet struct {
	A string
	B string
	C string
}
