package localfs

import "golang.org/x/sys/unix"

// Probe validates any existing provider-private root hierarchy without creating
// checkpoint content. Missing checkpoint-content state is healthy: the first
// explicit checkpoint mutation creates it lazily.
func Probe(stateDir string) error {
	stateFD, err := openPrivateRoot(stateDir)
	if err != nil {
		return err
	}
	defer unix.Close(stateFD)

	parent := stateFD
	owned := []int{}
	defer func() {
		for i := len(owned) - 1; i >= 0; i-- {
			_ = unix.Close(owned[i])
		}
	}()
	for _, name := range []string{"checkpoint-content", "v1", "checkpoints"} {
		fd, openErr := openPrivateDirAt(parent, name)
		if openErr != nil {
			if isNotExist(openErr) {
				return nil
			}
			return openErr
		}
		owned = append(owned, fd)
		parent = fd
	}
	return nil
}
