package contract_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maemreyo/shellbeam/internal/core/failure"
)

func TestMediaFailureTaxonomyContract(t *testing.T) {
	cases := []struct {
		code      failure.Code
		retryable bool
	}{
		{failure.FeatureUnavailable, false},
		{failure.InvalidInput, false},
		{failure.WorkspaceNotFound, false},
		{failure.CapacityExceeded, true},
		{failure.MediaPathNotFound, false},
		{failure.MediaPathUnsafe, false},
		{failure.MediaNotRegular, false},
		{failure.MediaTooLarge, false},
		{failure.MediaTypeUnsupported, false},
		{failure.MediaInvalidImage, false},
		{failure.MediaDimensionsExceeded, false},
		{failure.MediaSourceChanged, true},
		{failure.MediaReadTimeout, false},
		{failure.MediaReadFailed, false},
		{failure.InvalidDaemonResponse, false},
	}
	for _, tc := range cases {
		t.Run(string(tc.code), func(t *testing.T) {
			got := failure.Public(failure.New(tc.code, nil, errors.New("private /canonical/path errno payload")))
			if got.Code != tc.code || got.Retryable != tc.retryable {
				t.Fatalf("public=%#v want code=%s retryable=%v", got, tc.code, tc.retryable)
			}
			if got.Message == "" || strings.Contains(got.Message, "/canonical/path") || strings.Contains(got.Message, "errno") {
				t.Fatalf("unsafe public message: %#v", got)
			}
			for key, value := range got.Details {
				if strings.Contains(value, "/canonical/path") || strings.Contains(value, "errno") || strings.Contains(value, "payload") {
					t.Fatalf("unsafe public detail %s=%q for %s", key, value, tc.code)
				}
			}
		})
	}
	if got := failure.Public(errors.New("media_feature_unavailable")); got.Code == failure.Code("media_feature_unavailable") {
		t.Fatalf("forbidden media-specific feature code became public: %#v", got)
	}
}

func TestMediaPayloadTypesDoNotEnterPersistenceOrObservabilityProductionLayers(t *testing.T) {
	root := repoRoot(t)
	for _, rel := range []string{
		"internal/adapter/store",
		"internal/app/activity",
		"internal/app/evidence",
		"internal/app/observation",
		"internal/app/project",
		"internal/app/repro",
		"internal/app/telemetry",
		"internal/core/receipt",
		"internal/core/session",
		"internal/observability",
	} {
		base := filepath.Join(root, rel)
		_ = filepath.WalkDir(base, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					return filepath.SkipDir
				}
				t.Fatal(err)
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatal(readErr)
			}
			text := string(data)
			for _, forbidden := range []string{
				"github.com/maemreyo/shellbeam/internal/core/media",
				"media.Result",
				"media.File",
				"MediaRequest",
			} {
				if strings.Contains(text, forbidden) {
					t.Fatalf("media payload ownership leaked into persistence/observability layer %s via %q", path, forbidden)
				}
			}
			return nil
		})
	}
}

func TestMediaCorruptionCoverageRemainsMandatory(t *testing.T) {
	root := repoRoot(t)
	checks := map[string][]string{
		"internal/app/bridge/media_test.go": {
			"TestReadMediaRejectsDaemonAddressAndMetadataCorruption",
			"wrong-workspace", "wrong-kind", "normalized-path", "invalid-bytes",
			"schema-version", "TestReadMediaRejectsWrongOrCanonicalizedCWDSubstitution", "wrong-cwd", "canonicalized-cwd",
			"TestNewNegotiatedRejectsTamperedFingerprint",
		},
		"internal/adapter/ipc/media_test.go": {
			"TestMediaV2ClientRejectsReadErrorBeforeDecodingValidPrefix",
			"bad-base64", "short-response", "wrong-request", "wrong-action",
			"TestMediaV2ClientOuterResponseBoundary",
		},
		"internal/adapter/mcp/media_test.go": {
			"TestReadMediaBridgeCorruptionReturnsErrorWithoutImage",
			"TestReadMediaCallGateRequiresEffectiveNegotiation", "TestReadMediaInvalidAddressBasesReturnNoImage",
		},
	}
	for rel, required := range checks {
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatal(err)
		}
		for _, token := range required {
			if !strings.Contains(string(data), token) {
				t.Fatalf("%s no longer proves %q", rel, token)
			}
		}
	}
}

func TestMediaSecurityAndHardeningScriptsRunMandatoryScopes(t *testing.T) {
	root := repoRoot(t)
	checks := map[string][]string{
		"scripts/test-security.sh": {
			"./internal/adapter/localfs", "./internal/adapter/ipc", "./internal/adapter/mcp",
			"./internal/app/bridge", "./internal/app/daemon", "./internal/observability",
			"./tests/contract", "./tests/integration",
		},
		"scripts/test-hardening.sh": {
			"./internal/adapter/localfs", "./internal/adapter/ipc", "./internal/adapter/mcp",
			"./internal/app/bridge", "./internal/app/daemon", "./internal/observability",
			"./tests/contract", "./tests/integration",
		},
	}
	for rel, required := range checks {
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatal(err)
		}
		for _, scope := range required {
			if !strings.Contains(string(data), scope) {
				t.Errorf("%s missing mandatory media scope %s", rel, scope)
			}
		}
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	return root
}
