package capability

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"

	"github.com/maemreyo/shellbeam/internal/core/media"
)

type MediaSupport struct {
	SchemaVersion int      `json:"schema_version"`
	Kinds         []string `json:"kinds"`
	MIMETypes     []string `json:"mime_types"`
	MaxImageBytes int      `json:"max_image_bytes"`
	MaxWidth      int      `json:"max_width"`
	MaxHeight     int      `json:"max_height"`
	MaxPixels     int64    `json:"max_pixels"`
}

type NegotiatedMedia struct {
	Contract    MediaSupport `json:"contract"`
	Fingerprint string       `json:"fingerprint"`
}

func V1MediaSupport() MediaSupport {
	return MediaSupport{
		SchemaVersion: 1,
		Kinds:         []string{"image"},
		MIMETypes:     []string{"image/jpeg", "image/png", "image/webp"},
		MaxImageBytes: media.MaxImageBytes,
		MaxWidth:      media.MaxWidth,
		MaxHeight:     media.MaxHeight,
		MaxPixels:     media.MaxPixels,
	}
}

func (m MediaSupport) Clone() MediaSupport {
	out := m
	out.Kinds = append([]string(nil), m.Kinds...)
	out.MIMETypes = append([]string(nil), m.MIMETypes...)
	return out
}

func NegotiateMedia(consumer, daemon MediaSupport) (NegotiatedMedia, bool) {
	if consumer.SchemaVersion <= 0 || consumer.SchemaVersion != daemon.SchemaVersion || !validMediaLimits(consumer) || !validMediaLimits(daemon) {
		return NegotiatedMedia{}, false
	}
	contract := MediaSupport{
		SchemaVersion: consumer.SchemaVersion,
		Kinds:         intersectStrings(consumer.Kinds, daemon.Kinds),
		MIMETypes:     intersectStrings(consumer.MIMETypes, daemon.MIMETypes),
		MaxImageBytes: min(consumer.MaxImageBytes, daemon.MaxImageBytes),
		MaxWidth:      min(consumer.MaxWidth, daemon.MaxWidth),
		MaxHeight:     min(consumer.MaxHeight, daemon.MaxHeight),
		MaxPixels:     min(consumer.MaxPixels, daemon.MaxPixels),
	}
	if len(contract.Kinds) == 0 || len(contract.MIMETypes) == 0 || !contains(contract.Kinds, "image") {
		return NegotiatedMedia{}, false
	}
	encoded, err := json.Marshal(contract)
	if err != nil {
		return NegotiatedMedia{}, false
	}
	digest := sha256.Sum256(encoded)
	return NegotiatedMedia{Contract: contract, Fingerprint: hex.EncodeToString(digest[:])}, true
}

func validMediaLimits(s MediaSupport) bool {
	return s.MaxImageBytes > 0 && s.MaxWidth > 0 && s.MaxHeight > 0 && s.MaxPixels > 0
}

func intersectStrings(a, b []string) []string {
	other := make(map[string]struct{}, len(b))
	for _, value := range b {
		if value != "" {
			other[value] = struct{}{}
		}
	}
	seen := map[string]struct{}{}
	out := make([]string, 0)
	for _, value := range a {
		if value == "" {
			continue
		}
		if _, ok := other[value]; !ok {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
