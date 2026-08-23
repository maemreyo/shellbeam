package contextexec

// HelperRuntimeDirEnvironment carries private daemon rendezvous metadata to the
// prompt-launched helper. It is not helper claim authority and must be stripped
// before workload child exec.
const HelperRuntimeDirEnvironment = "SHELLBEAM_CONTEXT_EXEC_RUNTIME_DIR"
