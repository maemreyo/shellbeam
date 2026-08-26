#include "textflag.h"

TEXT libcPosixSpawn_trampoline<>(SB),NOSPLIT,$0-0
	JMP libc_posix_spawn(SB)
GLOBL ·libcPosixSpawnAddr(SB), RODATA, $8
DATA ·libcPosixSpawnAddr(SB)/8, $libcPosixSpawn_trampoline<>(SB)

TEXT libcSpawnAttrInit_trampoline<>(SB),NOSPLIT,$0-0
	JMP libc_spawnattr_init(SB)
GLOBL ·libcSpawnAttrInitAddr(SB), RODATA, $8
DATA ·libcSpawnAttrInitAddr(SB)/8, $libcSpawnAttrInit_trampoline<>(SB)

TEXT libcSpawnAttrDestroy_trampoline<>(SB),NOSPLIT,$0-0
	JMP libc_spawnattr_destroy(SB)
GLOBL ·libcSpawnAttrDestroyAddr(SB), RODATA, $8
DATA ·libcSpawnAttrDestroyAddr(SB)/8, $libcSpawnAttrDestroy_trampoline<>(SB)

TEXT libcSpawnAttrSetFlags_trampoline<>(SB),NOSPLIT,$0-0
	JMP libc_spawnattr_setflags(SB)
GLOBL ·libcSpawnAttrSetFlagsAddr(SB), RODATA, $8
DATA ·libcSpawnAttrSetFlagsAddr(SB)/8, $libcSpawnAttrSetFlags_trampoline<>(SB)

TEXT libcSpawnAttrSetPGroup_trampoline<>(SB),NOSPLIT,$0-0
	JMP libc_spawnattr_setpgroup(SB)
GLOBL ·libcSpawnAttrSetPGroupAddr(SB), RODATA, $8
DATA ·libcSpawnAttrSetPGroupAddr(SB)/8, $libcSpawnAttrSetPGroup_trampoline<>(SB)

TEXT libcSpawnActionsInit_trampoline<>(SB),NOSPLIT,$0-0
	JMP libc_spawn_actions_init(SB)
GLOBL ·libcSpawnActionsInitAddr(SB), RODATA, $8
DATA ·libcSpawnActionsInitAddr(SB)/8, $libcSpawnActionsInit_trampoline<>(SB)

TEXT libcSpawnActionsDestroy_trampoline<>(SB),NOSPLIT,$0-0
	JMP libc_spawn_actions_destroy(SB)
GLOBL ·libcSpawnActionsDestroyAddr(SB), RODATA, $8
DATA ·libcSpawnActionsDestroyAddr(SB)/8, $libcSpawnActionsDestroy_trampoline<>(SB)

TEXT libcSpawnActionsAddDup2_trampoline<>(SB),NOSPLIT,$0-0
	JMP libc_spawn_actions_adddup2(SB)
GLOBL ·libcSpawnActionsAddDup2Addr(SB), RODATA, $8
DATA ·libcSpawnActionsAddDup2Addr(SB)/8, $libcSpawnActionsAddDup2_trampoline<>(SB)

TEXT libcSpawnActionsAddClose_trampoline<>(SB),NOSPLIT,$0-0
	JMP libc_spawn_actions_addclose(SB)
GLOBL ·libcSpawnActionsAddCloseAddr(SB), RODATA, $8
DATA ·libcSpawnActionsAddCloseAddr(SB)/8, $libcSpawnActionsAddClose_trampoline<>(SB)

TEXT libcSpawnActionsAddChdir_trampoline<>(SB),NOSPLIT,$0-0
	JMP libc_spawn_actions_addchdir(SB)
GLOBL ·libcSpawnActionsAddChdirAddr(SB), RODATA, $8
DATA ·libcSpawnActionsAddChdirAddr(SB)/8, $libcSpawnActionsAddChdir_trampoline<>(SB)
