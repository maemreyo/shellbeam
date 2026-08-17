package localfs

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"golang.org/x/sys/unix"
)

type observationKind string

const (
	observationFile      observationKind = "file"
	observationSymlink   observationKind = "symlink"
	observationAbsent    observationKind = "absent"
	observationDirectory observationKind = "directory"
	observationSpecial   observationKind = "special"
)

type privateObservation struct {
	Path     string          `json:"path"`
	Kind     observationKind `json:"kind"`
	Identity string          `json:"identity,omitempty"`
	Size     int64           `json:"size,omitempty"`
	Mode     uint32          `json:"mode,omitempty"`
}

func observePath(rootFD int, rel string) (privateObservation, error) {
	parent, name, err := openParentAt(rootFD, rel)
	if err != nil {
		if isNotExist(err) {
			return privateObservation{Path: rel, Kind: observationAbsent}, nil
		}
		return privateObservation{}, err
	}
	defer unix.Close(parent)

	st, err := statAtNoFollow(parent, name)
	if err != nil {
		if isNotExist(err) {
			return privateObservation{Path: rel, Kind: observationAbsent}, nil
		}
		return privateObservation{}, err
	}
	switch fileType(st) {
	case unix.S_IFREG:
		return observeRegular(rootFD, rel)
	case unix.S_IFLNK:
		return observeSymlink(rootFD, rel)
	case unix.S_IFDIR:
		return privateObservation{Path: rel, Kind: observationDirectory}, nil
	default:
		return privateObservation{Path: rel, Kind: observationSpecial}, nil
	}
}

func observeRegular(rootFD int, rel string) (privateObservation, error) {
	data, mode, err := readRegularAt(rootFD, rel)
	if err != nil {
		return privateObservation{}, err
	}
	sum := sha256.Sum256(data)
	return privateObservation{
		Path: rel, Kind: observationFile, Identity: hex.EncodeToString(sum[:]),
		Size: int64(len(data)), Mode: mode & 0777,
	}, nil
}

func observeSymlink(rootFD int, rel string) (privateObservation, error) {
	text, err := readSymlinkAt(rootFD, rel)
	if err != nil {
		return privateObservation{}, err
	}
	sum := sha256.Sum256([]byte(text))
	return privateObservation{
		Path: rel, Kind: observationSymlink, Identity: hex.EncodeToString(sum[:]),
		Size: int64(len(text)),
	}, nil
}

func desiredObservation(entry privateEntry) (privateObservation, error) {
	switch entry.Kind {
	case entryFile:
		return privateObservation{
			Path: entry.Path, Kind: observationFile, Identity: entry.PrivateIdentity,
			Size: entry.Size, Mode: entry.Mode & 0777,
		}, nil
	case entrySymlink:
		return privateObservation{
			Path: entry.Path, Kind: observationSymlink, Identity: entry.PrivateIdentity,
			Size: entry.Size,
		}, nil
	case entryAbsent:
		return privateObservation{Path: entry.Path, Kind: observationAbsent}, nil
	case entryDirectory:
		return privateObservation{Path: entry.Path, Kind: observationDirectory}, nil
	default:
		return privateObservation{}, fmt.Errorf("unsupported checkpoint entry kind")
	}
}

func sameObservation(a, b privateObservation) bool {
	if a.Path != b.Path || a.Kind != b.Kind {
		return false
	}
	switch a.Kind {
	case observationFile:
		return a.Identity == b.Identity && a.Size == b.Size && a.Mode == b.Mode
	case observationSymlink:
		return a.Identity == b.Identity && a.Size == b.Size
	case observationAbsent, observationDirectory, observationSpecial:
		return true
	default:
		return false
	}
}

func observationMutationUnsupported(observation privateObservation) bool {
	return observation.Kind == observationDirectory || observation.Kind == observationSpecial
}

func validateObservation(observation privateObservation) error {
	if observation.Path == "" || observation.Size < 0 {
		return fmt.Errorf("invalid restore observation")
	}
	switch observation.Kind {
	case observationFile:
		if len(observation.Identity) != 64 || observation.Mode > 0777 {
			return fmt.Errorf("invalid regular restore observation")
		}
	case observationSymlink:
		if len(observation.Identity) != 64 {
			return fmt.Errorf("invalid symlink restore observation")
		}
	case observationAbsent, observationDirectory, observationSpecial:
		if observation.Identity != "" || observation.Size != 0 || observation.Mode != 0 {
			return fmt.Errorf("invalid simple restore observation")
		}
	default:
		return fmt.Errorf("invalid restore observation kind")
	}
	return nil
}
