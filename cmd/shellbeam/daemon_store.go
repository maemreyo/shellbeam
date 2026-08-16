package main

import (
	storeadapter "github.com/maemreyo/shellbeam/internal/adapter/store"
	"github.com/maemreyo/shellbeam/internal/config"
)

func openDaemonStore(stateDir string, cfg config.Config) (*storeadapter.Repository, error) {
	return storeadapter.Open(stateDir, storeadapter.Limits{
		MaxSessions: cfg.MaxConcurrentSessions, MaxSessionOutput: cfg.MaxSessionOutputBytes,
		MaxTotalState: cfg.MaxTotalStateBytes, ControlReserve: cfg.ControlReserveSessionBytes,
		MaxTelemetrySamples: telemetryMaxSamples, MaxTelemetryBytes: telemetryMetadataBytes,
		MaxTelemetryKeys: telemetryMaxKeys, MaxTelemetryKeysPerRepository: telemetryMaxKeysPerRepository,
		MaxTelemetrySamplesPerKey: telemetryMaxSamplesPerKey, MaxTelemetryAge: telemetryRetentionAge,
		MaxReproCapsules: reproMaxCapsules, MaxReproBytes: int64(reproMetadataBytes), MaxReproAge: reproRetentionAge,
	})
}
