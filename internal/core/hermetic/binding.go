package hermetic

import (
	"fmt"
	"reflect"
)

const BoundaryBindingSchemaV1 = 1

// BoundaryBinding is the durable, public identity frozen before spawn. It
// deliberately excludes provider-private paths, file descriptors, and wrapper
// arguments; those are execution mechanics, not authority facts.
type BoundaryBinding struct {
	SchemaVersion         int               `json:"schema_version"`
	BoundaryID            string            `json:"boundary_id"`
	Request               Request           `json:"request"`
	CaptureManifestSHA256 string            `json:"capture_manifest_sha256"`
	Provider              ProviderIdentity  `json:"provider"`
	Toolchain             ToolchainIdentity `json:"toolchain"`
}

func (b BoundaryBinding) Validate() error {
	if b.SchemaVersion != BoundaryBindingSchemaV1 || !boundaryIDPattern.MatchString(b.BoundaryID) || !validSHA256(b.CaptureManifestSHA256) {
		return fmt.Errorf("invalid hermetic boundary binding identity")
	}
	canonical, err := b.Request.Canonical()
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(canonical, b.Request) {
		return fmt.Errorf("noncanonical hermetic boundary request")
	}
	if err := b.Provider.Validate(); err != nil {
		return err
	}
	if err := b.Toolchain.Validate(); err != nil {
		return err
	}
	return nil
}

func (b *BoundaryBinding) Clone() *BoundaryBinding {
	if b == nil {
		return nil
	}
	out := *b
	out.Request = *b.Request.Clone()
	return &out
}

func ValidateBoundaryCompletion(binding BoundaryBinding, result BoundaryResult) error {
	if err := binding.Validate(); err != nil {
		return err
	}
	if err := result.Validate(); err != nil {
		return err
	}
	if result.BoundaryID != binding.BoundaryID || result.Provider != binding.Provider || result.Toolchain != binding.Toolchain {
		return fmt.Errorf("hermetic boundary result does not match frozen binding")
	}
	return nil
}
