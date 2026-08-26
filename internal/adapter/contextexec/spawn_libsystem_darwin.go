//go:build darwin

package contextexec

import (
	"fmt"
	"syscall"
	"unsafe"
	_ "unsafe"
)

const (
	darwinSpawnSetPGroup      = 0x0002
	darwinSpawnStartSuspended = 0x0080
)

type darwinSpawnAttr uintptr
type darwinSpawnActions uintptr

func darwinSyscall(fn, a1, a2, a3 uintptr) (r1, r2 uintptr, err syscall.Errno)
func darwinSyscall6(fn, a1, a2, a3, a4, a5, a6 uintptr) (r1, r2 uintptr, err syscall.Errno)

//go:linkname darwinSyscall syscall.syscall
//go:linkname darwinSyscall6 syscall.syscall6

var (
	libcPosixSpawnAddr           uintptr
	libcSpawnAttrInitAddr        uintptr
	libcSpawnAttrDestroyAddr     uintptr
	libcSpawnAttrSetFlagsAddr    uintptr
	libcSpawnAttrSetPGroupAddr   uintptr
	libcSpawnActionsInitAddr     uintptr
	libcSpawnActionsDestroyAddr  uintptr
	libcSpawnActionsAddDup2Addr  uintptr
	libcSpawnActionsAddCloseAddr uintptr
	libcSpawnActionsAddChdirAddr uintptr
)

//go:cgo_import_dynamic libc_posix_spawn posix_spawn "/usr/lib/libSystem.B.dylib"
//go:cgo_import_dynamic libc_spawnattr_init posix_spawnattr_init "/usr/lib/libSystem.B.dylib"
//go:cgo_import_dynamic libc_spawnattr_destroy posix_spawnattr_destroy "/usr/lib/libSystem.B.dylib"
//go:cgo_import_dynamic libc_spawnattr_setflags posix_spawnattr_setflags "/usr/lib/libSystem.B.dylib"
//go:cgo_import_dynamic libc_spawnattr_setpgroup posix_spawnattr_setpgroup "/usr/lib/libSystem.B.dylib"
//go:cgo_import_dynamic libc_spawn_actions_init posix_spawn_file_actions_init "/usr/lib/libSystem.B.dylib"
//go:cgo_import_dynamic libc_spawn_actions_destroy posix_spawn_file_actions_destroy "/usr/lib/libSystem.B.dylib"
//go:cgo_import_dynamic libc_spawn_actions_adddup2 posix_spawn_file_actions_adddup2 "/usr/lib/libSystem.B.dylib"
//go:cgo_import_dynamic libc_spawn_actions_addclose posix_spawn_file_actions_addclose "/usr/lib/libSystem.B.dylib"
//go:cgo_import_dynamic libc_spawn_actions_addchdir posix_spawn_file_actions_addchdir_np "/usr/lib/libSystem.B.dylib"

func darwinSpawnCall1(name string, fn, a1 uintptr) error {
	r1, _, errno := darwinSyscall(fn, a1, 0, 0)
	return darwinSpawnResult(name, r1, errno)
}

func darwinSpawnCall2(name string, fn, a1, a2 uintptr) error {
	r1, _, errno := darwinSyscall(fn, a1, a2, 0)
	return darwinSpawnResult(name, r1, errno)
}

func darwinSpawnCall3(name string, fn, a1, a2, a3 uintptr) error {
	r1, _, errno := darwinSyscall(fn, a1, a2, a3)
	return darwinSpawnResult(name, r1, errno)
}

func darwinSpawnResult(name string, result uintptr, errno syscall.Errno) error {
	if code := syscall.Errno(uint32(result)); code != 0 {
		return fmt.Errorf("%s: %w", name, code)
	}
	if errno != 0 {
		return fmt.Errorf("%s: %w", name, errno)
	}
	return nil
}

func darwinSpawnAttrInit(attr *darwinSpawnAttr) error {
	return darwinSpawnCall1("initialize spawn attributes", libcSpawnAttrInitAddr, uintptr(unsafe.Pointer(attr)))
}

func darwinSpawnAttrDestroy(attr *darwinSpawnAttr) error {
	if attr == nil || *attr == 0 {
		return nil
	}
	err := darwinSpawnCall1("destroy spawn attributes", libcSpawnAttrDestroyAddr, uintptr(unsafe.Pointer(attr)))
	*attr = 0
	return err
}

func darwinSpawnAttrSetFlags(attr *darwinSpawnAttr, flags uintptr) error {
	return darwinSpawnCall2("set spawn flags", libcSpawnAttrSetFlagsAddr, uintptr(unsafe.Pointer(attr)), flags)
}

func darwinSpawnAttrSetPGroup(attr *darwinSpawnAttr, group int) error {
	return darwinSpawnCall2("set spawn process group", libcSpawnAttrSetPGroupAddr, uintptr(unsafe.Pointer(attr)), uintptr(group))
}

func darwinSpawnActionsInit(actions *darwinSpawnActions) error {
	return darwinSpawnCall1("initialize spawn file actions", libcSpawnActionsInitAddr, uintptr(unsafe.Pointer(actions)))
}

func darwinSpawnActionsDestroy(actions *darwinSpawnActions) error {
	if actions == nil || *actions == 0 {
		return nil
	}
	err := darwinSpawnCall1("destroy spawn file actions", libcSpawnActionsDestroyAddr, uintptr(unsafe.Pointer(actions)))
	*actions = 0
	return err
}

func darwinSpawnActionsAddDup2(actions *darwinSpawnActions, from uintptr, to int) error {
	return darwinSpawnCall3("add spawn dup2 action", libcSpawnActionsAddDup2Addr, uintptr(unsafe.Pointer(actions)), from, uintptr(to))
}

func darwinSpawnActionsAddClose(actions *darwinSpawnActions, fd uintptr) error {
	return darwinSpawnCall2("add spawn close action", libcSpawnActionsAddCloseAddr, uintptr(unsafe.Pointer(actions)), fd)
}

func darwinSpawnActionsAddChdir(actions *darwinSpawnActions, path *byte) error {
	return darwinSpawnCall2("add spawn chdir action", libcSpawnActionsAddChdirAddr, uintptr(unsafe.Pointer(actions)), uintptr(unsafe.Pointer(path)))
}

func darwinPosixSpawn(path *byte, actions *darwinSpawnActions, attr *darwinSpawnAttr, argv, env []*byte) (int, error) {
	var pid int32
	r1, _, errno := darwinSyscall6(
		libcPosixSpawnAddr,
		uintptr(unsafe.Pointer(&pid)),
		uintptr(unsafe.Pointer(path)),
		uintptr(unsafe.Pointer(actions)),
		uintptr(unsafe.Pointer(attr)),
		darwinCStringVectorPointer(argv),
		darwinCStringVectorPointer(env),
	)
	if err := darwinSpawnResult("spawn suspended context child", r1, errno); err != nil {
		return 0, err
	}
	return int(pid), nil
}
