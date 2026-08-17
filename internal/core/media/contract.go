package media

import "time"

type AddressKind string

const (
	AddressWorkspace AddressKind = "workspace"
	AddressCWD       AddressKind = "cwd"

	MaxImageBytes               = 7 << 20
	MaxWidth                    = 16384
	MaxHeight                   = 16384
	MaxPixels             int64 = 40_000_000
	MaxOuterResponseBytes       = 9_852_248
	MaxPathBytes                = 1024
	MaxCWDBytes                 = 1024
	MaxPathComponents           = 64
	MaxConcurrentReads          = 2
	AcquisitionBudget           = 5 * time.Second
)

type Limits struct {
	MaxImageBytes int
	MaxWidth      int
	MaxHeight     int
	MaxPixels     int64
}

func V1Limits() Limits {
	return Limits{MaxImageBytes: MaxImageBytes, MaxWidth: MaxWidth, MaxHeight: MaxHeight, MaxPixels: MaxPixels}
}

type DisplayAddress struct {
	AddressKind AddressKind `json:"address_kind"`
	WorkspaceID string      `json:"workspace_id,omitempty"`
	CWD         string      `json:"cwd,omitempty"`
	Path        string      `json:"path"`
}

func (a DisplayAddress) Validate() error {
	if _, err := ParseLogicalPath(a.Path); err != nil {
		return err
	}
	switch a.AddressKind {
	case AddressWorkspace:
		if a.WorkspaceID == "" || a.CWD != "" {
			return errInvalidDisplayAddress
		}
	case AddressCWD:
		if a.WorkspaceID != "" || ValidateCWD(a.CWD) != nil {
			return errInvalidDisplayAddress
		}
	default:
		return errInvalidDisplayAddress
	}
	return nil
}

type File struct {
	MIMEType string
	Format   string
	Width    int
	Height   int
	Data     []byte
}

type Result struct {
	SchemaVersion  int            `json:"schema_version"`
	Kind           string         `json:"kind"`
	DisplayAddress DisplayAddress `json:"display_address"`
	MIMEType       string         `json:"mime_type"`
	Format         string         `json:"format"`
	ByteSize       int            `json:"byte_size"`
	Width          int            `json:"width"`
	Height         int            `json:"height"`
	Data           []byte         `json:"data"`
}
