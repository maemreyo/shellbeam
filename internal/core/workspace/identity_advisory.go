package workspace

import (
	"crypto/sha256"
	"encoding/hex"
	gitidentity "github.com/maemreyo/shellbeam/internal/core/gitidentity"
)

func AdvisoryFromIdentityFinding(id WorkspaceID, finding gitidentity.Finding) Advisory {
	normalized := finding.Code + "|" + finding.Facts["profile"] + "|" + finding.Facts["resolution_source"]
	sum := sha256.Sum256([]byte(normalized))
	return Advisory{
		Code:             finding.Code,
		Severity:         finding.Severity,
		Message:          finding.Message,
		WorkspaceID:      id,
		CauseFingerprint: hex.EncodeToString(sum[:]),
	}
}
