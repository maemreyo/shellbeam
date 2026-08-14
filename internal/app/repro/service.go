package repro

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"time"

	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	core "github.com/maemreyo/shellbeam/internal/core/repro"
	structured "github.com/maemreyo/shellbeam/internal/core/structuredresult"
	telemetry "github.com/maemreyo/shellbeam/internal/core/telemetry"
	"github.com/oklog/ulid/v2"
)

const (
	telemetryProducerID      = "shellbeam.telemetry"
	telemetryProducerVersion = 1
)

type Service struct {
	repository Repository
	now        func() time.Time
	newReproID func() string
}

func New(repository Repository) *Service {
	return &Service{
		repository: repository,
		now:        func() time.Time { return time.Now().UTC() },
		newReproID: func() string { return "repro_" + ulid.Make().String() },
	}
}

type producerCut struct {
	Kind            string                 `json:"kind"`
	Availability    core.AvailabilityState `json:"availability"`
	RefID           string                 `json:"ref_id,omitempty"`
	ProducerID      string                 `json:"producer_id,omitempty"`
	ProducerVersion int                    `json:"producer_version,omitempty"`
	SchemaVersion   int                    `json:"schema_version,omitempty"`
	Digest          string                 `json:"digest,omitempty"`
	Lifecycle       string                 `json:"lifecycle,omitempty"`
	Completeness    string                 `json:"completeness,omitempty"`
}

type captureCut struct {
	SchemaVersion int                        `json:"schema_version"`
	Execution     core.ExecutionDescriptor   `json:"execution"`
	Source        core.SourceDescriptor      `json:"source"`
	Project       core.ProjectDescriptor     `json:"project"`
	Environment   core.EnvironmentDescriptor `json:"environment"`
	Input         core.InputDescriptor       `json:"input"`
	Results       []core.ReferenceDescriptor `json:"results"`
	Producers     []producerCut              `json:"producers"`
}

func (s *Service) Create(ctx context.Context, request core.CreateRequest) (core.Capsule, error) {
	if s == nil || s.repository == nil {
		return core.Capsule{}, failure.New(failure.ReproMaterializationUnavailable, map[string]string{"operation_id": request.OperationID, "reason": "repository_unavailable"}, nil)
	}
	if err := request.Validate(); err != nil {
		return core.Capsule{}, err
	}
	fingerprint, err := request.Fingerprint()
	if err != nil {
		return core.Capsule{}, err
	}
	if existing, found, err := s.repository.GetReproByCreateID(ctx, request.CreateID); err != nil {
		return core.Capsule{}, err
	} else if found {
		winner, _, err := s.repository.CreateRepro(ctx, fingerprint, existing)
		return winner, err
	}

	reservation, rec, receiptDigest, err := s.authoritativeExecution(ctx, request.OperationID)
	if err != nil {
		return core.Capsule{}, err
	}
	execution, err := commandDescriptor(reservation, rec.ExecutionFingerprint, rec.ExecutionMode, rec.Executable)
	if err != nil {
		return core.Capsule{}, err
	}
	execution.ReceiptDigest = receiptDigest

	structuredDerivation, structuredFound, err := s.repository.FindOperationDerivation(ctx, request.OperationID)
	if err != nil {
		return core.Capsule{}, err
	}
	telemetryRecord, telemetryFound, err := s.repository.FindPerformanceByOperation(ctx, request.OperationID)
	if err != nil {
		return core.Capsule{}, err
	}
	if structuredFound {
		if err := structuredDerivation.Validate(); err != nil {
			return core.Capsule{}, failure.New(failure.ReproMaterializationUnavailable, map[string]string{"operation_id": request.OperationID, "reason": "structured_state_invalid"}, err)
		}
	}
	if telemetryFound {
		if err := telemetryRecord.Validate(); err != nil || telemetryRecord.OperationID != request.OperationID || telemetryRecord.SessionID != rec.SessionID || telemetryRecord.ReceiptDigest != receiptDigest {
			return core.Capsule{}, failure.New(failure.ReproMaterializationUnavailable, map[string]string{"operation_id": request.OperationID, "reason": "telemetry_state_invalid"}, err)
		}
		execution.ProjectCommandID = telemetryRecord.ProjectCommandID
		execution.ParameterBindingFingerprint = telemetryRecord.ParameterBindingFingerprint
	}

	source := sourceDescriptor(rec, telemetryRecord, telemetryFound)
	environment := environmentDescriptor(telemetryRecord, telemetryFound)
	input := core.InputDescriptor{
		AcceptedBytes: rec.InputAcceptedBytes, DeliveredBytes: rec.InputDeliveredBytes,
		Complete: rec.InputAcceptedBytes == rec.InputDeliveredBytes, ContentIdentity: core.CaptureUnavailable,
	}
	project := core.ProjectDescriptor{Quality: core.CaptureUnavailable}
	results, producers := currentResultDescriptors(request.OperationID, reservation, structuredDerivation, structuredFound, telemetryRecord, telemetryFound)
	cut := captureCut{SchemaVersion: 1, Execution: execution, Source: source, Project: project, Environment: environment, Input: input, Results: results, Producers: producers}
	cutDigest, err := digestCaptureCut(cut)
	if err != nil {
		return core.Capsule{}, err
	}
	capsule := core.Capsule{
		SchemaVersion: core.SchemaVersion, CreateID: request.CreateID, ReproID: s.newReproID(), CreatedAt: s.now(), CaptureCutDigest: cutDigest,
		Execution: execution, Source: source, Project: project, Environment: environment, Input: input, Results: results,
		Capture: core.CaptureMatrix{
			Source: source.Quality, Command: execution.CommandDetails,
			Toolchain: environment.ToolchainQuality, Environment: environment.EnvironmentQuality,
			FilesystemExternal: core.CaptureUnknown, NetworkDependencies: core.CaptureUnknown,
			ExternalServices: core.CaptureUnknown, TimeRandomness: core.CaptureUnknown,
			Input: core.CapturePartial, Results: core.CaptureComplete,
		},
	}
	if err := capsule.Validate(); err != nil {
		return core.Capsule{}, err
	}
	winner, _, err := s.repository.CreateRepro(ctx, fingerprint, capsule)
	return winner, err
}

func (s *Service) authoritativeExecution(ctx context.Context, operationID string) (operation.Reservation, receipt.Receipt, string, error) {
	id, err := operation.ParseID(operationID)
	if err != nil {
		return operation.Reservation{}, receipt.Receipt{}, "", err
	}
	reservation, err := s.repository.LoadOperation(ctx, id)
	if err != nil {
		return operation.Reservation{}, receipt.Receipt{}, "", failure.New(failure.ReproMaterializationUnavailable, map[string]string{"operation_id": operationID, "reason": "operation_unavailable"}, err)
	}
	if reservation.OperationID != id || reservation.SessionID == "" {
		return operation.Reservation{}, receipt.Receipt{}, "", failure.New(failure.ReproMaterializationUnavailable, map[string]string{"operation_id": operationID, "reason": "operation_mismatch"}, nil)
	}
	rec, err := s.repository.LoadReceipt(ctx, reservation.SessionID)
	if err != nil {
		return operation.Reservation{}, receipt.Receipt{}, "", failure.New(failure.ReproMaterializationUnavailable, map[string]string{"operation_id": operationID, "reason": "receipt_unavailable"}, err)
	}
	if err := rec.Validate(); err != nil || !rec.State.Terminal() || rec.OperationID != operationID || rec.SessionID != string(reservation.SessionID) || rec.ExecutionFingerprint == "" {
		return operation.Reservation{}, receipt.Receipt{}, "", failure.New(failure.ReproMaterializationUnavailable, map[string]string{"operation_id": operationID, "reason": "receipt_not_terminal_authority"}, err)
	}
	if reservation.ExecutionFingerprint != "" && reservation.ExecutionFingerprint != rec.ExecutionFingerprint {
		return operation.Reservation{}, receipt.Receipt{}, "", failure.New(failure.ReproMaterializationUnavailable, map[string]string{"operation_id": operationID, "reason": "execution_fingerprint_mismatch"}, nil)
	}
	digest, err := receipt.Digest(rec)
	if err != nil {
		return operation.Reservation{}, receipt.Receipt{}, "", err
	}
	return reservation, rec, digest, nil
}

func currentResultDescriptors(operationID string, reservation operation.Reservation, d structured.Derivation, structuredFound bool, t telemetry.PerformanceRecord, telemetryFound bool) ([]core.ReferenceDescriptor, []producerCut) {
	results := make([]core.ReferenceDescriptor, 0, 2)
	producers := make([]producerCut, 0, 2)
	if structuredFound {
		availability := core.AvailabilityPending
		if d.Lifecycle == structured.LifecycleTerminal {
			availability = core.AvailabilityTerminal
		}
		ref := core.ReferenceDescriptor{RefID: "structured:" + d.DerivationKey, RecordKind: "structured_result", ProducerID: d.Producer.AdapterID, ProducerVersion: d.Producer.AdapterVersion, SchemaVersion: d.SchemaVersion, Digest: d.DerivationKey, OriginalAvailability: availability}
		results = append(results, ref)
		producers = append(producers, producerCut{Kind: ref.RecordKind, Availability: availability, RefID: ref.RefID, ProducerID: ref.ProducerID, ProducerVersion: ref.ProducerVersion, SchemaVersion: ref.SchemaVersion, Digest: ref.Digest, Lifecycle: string(d.Lifecycle), Completeness: string(d.Completeness)})
	} else {
		producers = append(producers, producerCut{Kind: "structured_result", Availability: core.AvailabilityAbsent, ProducerID: reservation.StructuredAdapter})
	}
	if telemetryFound {
		ref := core.ReferenceDescriptor{RefID: "telemetry:" + t.DerivationKey, RecordKind: "execution_telemetry", ProducerID: t.Producer.ProducerID, ProducerVersion: t.Producer.ProducerVersion, SchemaVersion: t.SchemaVersion, Digest: t.DerivationKey, OriginalAvailability: core.AvailabilityTerminal}
		results = append(results, ref)
		producers = append(producers, producerCut{Kind: ref.RecordKind, Availability: core.AvailabilityTerminal, RefID: ref.RefID, ProducerID: ref.ProducerID, ProducerVersion: ref.ProducerVersion, SchemaVersion: ref.SchemaVersion, Digest: ref.Digest, Lifecycle: string(t.Lifecycle), Completeness: string(t.Completeness)})
	} else {
		ref := core.ReferenceDescriptor{RefID: "telemetry:operation:" + operationID, RecordKind: "execution_telemetry", ProducerID: telemetryProducerID, ProducerVersion: telemetryProducerVersion, SchemaVersion: telemetry.SchemaVersion, OriginalAvailability: core.AvailabilityAbsent}
		results = append(results, ref)
		producers = append(producers, producerCut{Kind: ref.RecordKind, Availability: core.AvailabilityAbsent, RefID: ref.RefID, ProducerID: ref.ProducerID, ProducerVersion: ref.ProducerVersion, SchemaVersion: ref.SchemaVersion})
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].RecordKind == results[j].RecordKind {
			return results[i].RefID < results[j].RefID
		}
		return results[i].RecordKind < results[j].RecordKind
	})
	sort.Slice(producers, func(i, j int) bool {
		if producers[i].Kind == producers[j].Kind {
			return producers[i].RefID < producers[j].RefID
		}
		return producers[i].Kind < producers[j].Kind
	})
	return results, producers
}

func sourceDescriptor(rec receipt.Receipt, t telemetry.PerformanceRecord, telemetryFound bool) core.SourceDescriptor {
	d := core.SourceDescriptor{Quality: core.CaptureUnavailable}
	if rec.WorkspaceProvenance != nil {
		p := rec.WorkspaceProvenance
		switch p.SchemaVersion {
		case 1:
			d.RepositoryID, d.WorkspaceID = string(p.RepositoryID), string(p.WorkspaceID)
			if p.PostGeneration != "" {
				d.WorkspaceGeneration = p.PostGeneration
			} else {
				d.WorkspaceGeneration = p.PreGeneration
			}
		case 2:
			d.RepositoryID, d.WorkspaceID = string(p.Binding.RepositoryID), string(p.Binding.WorkspaceID)
			if p.Post.Generation != "" {
				d.WorkspaceGeneration = p.Post.Generation
			} else {
				d.WorkspaceGeneration = p.Pre.Generation
			}
		}
		if d.RepositoryID == "" {
			d.WorkspaceID = ""
		}
		if d.RepositoryID != "" || d.WorkspaceGeneration != "" {
			d.Quality = core.CapturePartial
		}
	}
	if telemetryFound && t.SourceContentDigest != "" {
		d.SourceContentDigest = t.SourceContentDigest
		d.Quality = core.CapturePartial
	}
	return d
}

func environmentDescriptor(t telemetry.PerformanceRecord, found bool) core.EnvironmentDescriptor {
	d := core.EnvironmentDescriptor{EnvironmentQuality: core.CaptureUnavailable, ToolchainQuality: core.CaptureUnavailable}
	if !found {
		return d
	}
	if t.EnvironmentFingerprint != "" {
		d.EnvironmentFingerprint, d.EnvironmentSchemaVersion, d.EnvironmentQuality = t.EnvironmentFingerprint, t.EnvironmentSchemaVersion, core.CaptureExact
	}
	if t.ToolchainFingerprint != "" {
		d.ToolchainFingerprint, d.ToolchainSchemaVersion, d.ToolchainQuality = t.ToolchainFingerprint, t.ToolchainSchemaVersion, core.CaptureExact
	}
	return d
}

func digestCaptureCut(cut captureCut) (string, error) {
	encoded, err := json.Marshal(cut)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}
