package capability

import (
	"reflect"
	"testing"

	"github.com/maemreyo/shellbeam/internal/core/media"
)

func TestV1MediaSupportIsExactReviewedContract(t *testing.T) {
	got := V1MediaSupport()
	want := MediaSupport{SchemaVersion: 1, Kinds: []string{"image"}, MIMETypes: []string{"image/jpeg", "image/png", "image/webp"}, MaxImageBytes: media.MaxImageBytes, MaxWidth: media.MaxWidth, MaxHeight: media.MaxHeight, MaxPixels: media.MaxPixels}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got=%#v want=%#v", got, want)
	}
}

func TestNegotiateMediaIntersectsSortedMIMEsAndLowerLimits(t *testing.T) {
	consumer := V1MediaSupport()
	consumer.MIMETypes = []string{"image/webp", "image/png"}
	consumer.MaxImageBytes = 5 << 20
	consumer.MaxWidth = 12000
	daemon := V1MediaSupport()
	daemon.MIMETypes = []string{"image/jpeg", "image/png", "image/webp"}
	daemon.MaxHeight = 8000
	daemon.MaxPixels = 20_000_000
	got, ok := NegotiateMedia(consumer, daemon)
	if !ok {
		t.Fatal("negotiation failed")
	}
	if !reflect.DeepEqual(got.Contract.MIMETypes, []string{"image/png", "image/webp"}) || !reflect.DeepEqual(got.Contract.Kinds, []string{"image"}) {
		t.Fatalf("contract=%#v", got.Contract)
	}
	if got.Contract.MaxImageBytes != 5<<20 || got.Contract.MaxWidth != 12000 || got.Contract.MaxHeight != 8000 || got.Contract.MaxPixels != 20_000_000 {
		t.Fatalf("limits=%#v", got.Contract)
	}
	if len(got.Fingerprint) != 64 {
		t.Fatalf("fingerprint=%q", got.Fingerprint)
	}
}

func TestNegotiateMediaRejectsMismatchOrEmptyIntersection(t *testing.T) {
	base := V1MediaSupport()
	bad := base
	bad.SchemaVersion = 2
	if _, ok := NegotiateMedia(base, bad); ok {
		t.Fatal("schema mismatch negotiated")
	}
	bad = base
	bad.MIMETypes = []string{"image/gif"}
	if _, ok := NegotiateMedia(base, bad); ok {
		t.Fatal("empty MIME intersection negotiated")
	}
	bad = base
	bad.Kinds = []string{"document"}
	if _, ok := NegotiateMedia(base, bad); ok {
		t.Fatal("empty kind intersection negotiated")
	}
	bad = base
	bad.MaxImageBytes = 0
	if _, ok := NegotiateMedia(base, bad); ok {
		t.Fatal("zero limit negotiated")
	}
}

func TestNegotiateMediaFingerprintDeterministicAcrossInputOrder(t *testing.T) {
	a := V1MediaSupport()
	b := V1MediaSupport()
	a.MIMETypes = []string{"image/webp", "image/png", "image/jpeg"}
	a.Kinds = []string{"image"}
	b.MIMETypes = []string{"image/jpeg", "image/webp", "image/png"}
	one, ok := NegotiateMedia(a, b)
	if !ok {
		t.Fatal("one")
	}
	two, ok := NegotiateMedia(b, a)
	if !ok {
		t.Fatal("two")
	}
	if one.Fingerprint != two.Fingerprint || !reflect.DeepEqual(one.Contract, two.Contract) {
		t.Fatalf("one=%#v two=%#v", one, two)
	}
}

func TestMediaCatalogCloneIsolationAndBaselineOmission(t *testing.T) {
	base := Baseline(Limits{})
	if base.Features[FeatureRichLocalMedia] != Unavailable || base.Media != nil {
		t.Fatalf("baseline leaked media: %#v", base)
	}
	support := V1MediaSupport()
	base.Media = &support
	base.Features[FeatureRichLocalMedia] = Available
	clone := base.Clone()
	clone.Media.MIMETypes[0] = "changed"
	clone.Media.Kinds[0] = "changed"
	if base.Media.MIMETypes[0] == "changed" || base.Media.Kinds[0] == "changed" {
		t.Fatal("media clone aliases slices")
	}
}
