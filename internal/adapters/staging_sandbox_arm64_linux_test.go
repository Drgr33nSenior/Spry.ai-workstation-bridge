//go:build linux && arm64

package adapters

import "syscall"

// arm64 has no legacy open/creat/mknod entry points. These are the applicable
// native syscall forms used by the Go runtime and os.Root implementation.
func sandboxSxidRules() []sxidRule {
	return []sxidRule{
		{syscall.SYS_FCHMOD, 1}, {syscall.SYS_FCHMODAT, 2}, {452, 2}, {syscall.SYS_MKDIRAT, 2}, {syscall.SYS_MKNODAT, 2}, {syscall.SYS_OPENAT, 3},
	}
}

func sandboxAuditArchitecture() uint32 { return 0xc00000b7 }
func sandboxOpenat2Syscall() uint32    { return 437 }
func sandboxX32SyscallBit() uint32     { return 0 }
func sandboxSeccompSyscall() uintptr   { return 277 }
