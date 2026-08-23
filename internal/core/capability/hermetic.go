package capability

type HermeticBoundarySupport struct {
	Version            int    `json:"version"`
	Maturity           string `json:"maturity"`
	Provider           string `json:"provider"`
	ProviderVersion    string `json:"provider_version"`
	Scope              string `json:"scope"`
	Filesystem         string `json:"filesystem"`
	Network            string `json:"network"`
	Environment        string `json:"environment"`
	Stdin              string `json:"stdin"`
	Writes             string `json:"writes"`
	TimeRandomness     string `json:"time_randomness"`
	ChildTree          string `json:"child_tree"`
	Placement          string `json:"placement"`
	PTY                string `json:"pty"`
	PersistentSessions string `json:"persistent_sessions"`
	Authority          string `json:"authority"`
}

func (s HermeticBoundarySupport) ValidV1() bool {
	return s.Version == 1 && s.Maturity == "experimental" &&
		s.Provider == "bubblewrap" && s.ProviderVersion == "0.11.2" &&
		s.Scope == "verification_only_ephemeral" && s.Filesystem == "immutable_capture" &&
		s.Network == "off" && s.Environment == "fixed_allowlist" && s.Stdin == "closed" &&
		s.Writes == "ephemeral_discard" && s.TimeRandomness == "ambient_nondeterministic" &&
		s.ChildTree == "enclosed" && s.Placement == "pre_exec" && s.PTY == "unsupported" &&
		s.PersistentSessions == "unsupported" && s.Authority == "proven_input_scope"
}
