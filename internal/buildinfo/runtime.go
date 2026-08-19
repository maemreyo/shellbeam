package buildinfo

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
)

type ProcessIdentity struct {
	Version      string
	Revision     string
	VCSModified  *bool
	BinarySHA256 string
}

func CaptureProcessIdentity() ProcessIdentity {
	return captureProcessIdentity(debug.ReadBuildInfo, openCurrentExecutable, Current())
}

func openCurrentExecutable() (io.ReadCloser, error) {
	path, err := os.Executable()
	if err != nil {
		return nil, err
	}
	return os.Open(path)
}

func captureProcessIdentity(
	readBuildInfo func() (*debug.BuildInfo, bool),
	openExecutable func() (io.ReadCloser, error),
	linker Info,
) ProcessIdentity {
	identity := ProcessIdentity{
		Version:  normalizeBuildValue(linker.Version),
		Revision: normalizeBuildValue(linker.Commit),
	}

	if info, ok := readBuildInfo(); ok && info != nil {
		if version := normalizeBuildValue(info.Main.Version); version != "" {
			identity.Version = version
		}
		for _, setting := range info.Settings {
			switch setting.Key {
			case "vcs.revision":
				if revision := normalizeBuildValue(setting.Value); revision != "" {
					identity.Revision = revision
				}
			case "vcs.modified":
				if modified, err := strconv.ParseBool(setting.Value); err == nil {
					value := modified
					identity.VCSModified = &value
				}
			}
		}
	}

	if reader, err := openExecutable(); err == nil && reader != nil {
		hasher := sha256.New()
		if _, copyErr := io.Copy(hasher, reader); copyErr == nil {
			identity.BinarySHA256 = hex.EncodeToString(hasher.Sum(nil))
		}
		_ = reader.Close()
	}
	return identity
}

func normalizeBuildValue(value string) string {
	value = strings.TrimSpace(value)
	switch strings.ToLower(value) {
	case "", "dev", "unknown", "(devel)":
		return ""
	default:
		return value
	}
}
