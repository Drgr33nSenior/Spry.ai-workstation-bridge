//go:build linux && amd64

package adapters

import "syscall"

func sandboxSxidRules() []sxidRule {
	return []sxidRule{
		{syscall.SYS_CHMOD, 1}, {syscall.SYS_FCHMOD, 1}, {syscall.SYS_FCHMODAT, 2}, {452, 2},
		{syscall.SYS_MKDIR, 1}, {syscall.SYS_MKDIRAT, 2}, {syscall.SYS_MKNOD, 1}, {syscall.SYS_MKNODAT, 2},
		{syscall.SYS_OPEN, 2}, {syscall.SYS_CREAT, 1}, {syscall.SYS_OPENAT, 3},
	}
}

func sandboxAuditArchitecture() uint32 { return 0xc000003e }
func sandboxOpenat2Syscall() uint32    { return 437 }
func sandboxX32SyscallBit() uint32     { return 0x40000000 }
func sandboxSeccompSyscall() uintptr   { return 317 }
