//go:build linux && (amd64 || arm64)

package adapters

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/catalog"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/domain"
)

// The rule set mirrors systemd's seccomp_restrict_sxid mode-bit checks for the
// tested architecture. Source checked 2026-09-08: systemd commit
// ce04f8a331a54ea6c15d602b56791dbfa4785b70,
// src/shared/seccomp-util.c:seccomp_restrict_sxid.
type sxidRule struct{ syscall, modeArgument uint32 }

type sandboxBPFInstruction struct {
	Code uint16
	Jt   uint8
	Jf   uint8
	K    uint32
}
type sandboxBPFProgram struct {
	Len    uint16
	Filter *sandboxBPFInstruction
}

const (
	sandboxBPFLoadWordAbsolute  = 0x20
	sandboxBPFJumpEqual         = 0x15
	sandboxBPFJumpSet           = 0x45
	sandboxBPFAnd               = 0x54
	sandboxBPFReturn            = 0x06
	sandboxSeccompDataArgs      = 16
	sandboxSeccompDataArch      = 4
	sandboxSeccompAllow         = 0x7fff0000
	sandboxSeccompErrno         = 0x00050000
	sandboxPRSetNoNewPrivs      = 38
	sandboxSeccompSetModeFilter = 1
	sandboxSeccompFilterTSYNC   = 1
	sandboxModeSxid             = 06000
)

func installRestrictSUIDSGIDFilter() error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	// For every syscall, inspect the mode argument and deny only attempts to
	// set S_ISUID or S_ISGID. This permits normal 0640/0750 publication
	// modes while catching the chmod/setgid dependency that systemd rejects.
	program := []sandboxBPFInstruction{
		{Code: sandboxBPFLoadWordAbsolute, K: sandboxSeccompDataArch},
		{Code: sandboxBPFJumpEqual, Jt: 1, K: sandboxAuditArchitecture()},
		{Code: sandboxBPFReturn, K: sandboxSeccompErrno | uint32(syscall.EPERM)},
		{Code: sandboxBPFLoadWordAbsolute, K: 0},
		{Code: sandboxBPFJumpSet, Jf: 1, K: sandboxX32SyscallBit()},
		{Code: sandboxBPFReturn, K: sandboxSeccompErrno | uint32(syscall.EPERM)},
	}
	for _, rule := range sandboxSxidRules() {
		program = append(program,
			sandboxBPFInstruction{Code: sandboxBPFLoadWordAbsolute, K: 0},
			sandboxBPFInstruction{Code: sandboxBPFJumpEqual, Jf: 4, K: rule.syscall},
			sandboxBPFInstruction{Code: sandboxBPFLoadWordAbsolute, K: sandboxSeccompDataArgs + 8*rule.modeArgument},
			sandboxBPFInstruction{Code: sandboxBPFAnd, K: sandboxModeSxid},
			sandboxBPFInstruction{Code: sandboxBPFJumpEqual, Jt: 1, K: 0},
			sandboxBPFInstruction{Code: sandboxBPFReturn, K: sandboxSeccompErrno | uint32(syscall.EPERM)},
		)
	}
	program = append(program,
		sandboxBPFInstruction{Code: sandboxBPFLoadWordAbsolute, K: 0},
		sandboxBPFInstruction{Code: sandboxBPFJumpEqual, Jf: 1, K: sandboxOpenat2Syscall()},
		sandboxBPFInstruction{Code: sandboxBPFReturn, K: sandboxSeccompErrno | uint32(syscall.ENOSYS)},
	)
	program = append(program, sandboxBPFInstruction{Code: sandboxBPFReturn, K: sandboxSeccompAllow})
	if len(program) > 0xffff {
		return errors.New("seccomp test program exceeds kernel limit")
	}
	if _, _, errno := syscall.Syscall6(syscall.SYS_PRCTL, sandboxPRSetNoNewPrivs, 1, 0, 0, 0, 0); errno != 0 {
		return errno
	}
	filter := sandboxBPFProgram{Len: uint16(len(program)), Filter: &program[0]}
	ret, _, errno := syscall.Syscall6(sandboxSeccompSyscall(), sandboxSeccompSetModeFilter, sandboxSeccompFilterTSYNC, uintptr(unsafe.Pointer(&filter)), 0, 0, 0)
	if errno != 0 {
		return errno
	}
	if ret != 0 {
		return errors.New("seccomp TSYNC did not synchronize every Go thread")
	}
	return nil
}

// TestStagingRestrictSUIDSGID is opt-in because it irreversibly installs a
// filter in this test process. Run as a separately created unprivileged writer
// in a disposable Linux container, never against a workstation model root.
func TestStagingRestrictSUIDSGID(t *testing.T) {
	if os.Getenv("BRIDGE_STAGING_SANDBOX") != "1" {
		t.Skip("NOT RUN — requires an isolated Linux writer process with BRIDGE_STAGING_SANDBOX=1")
	}
	root := os.Getenv("BRIDGE_STAGING_SANDBOX_ROOT")
	readerGID, err := strconv.Atoi(os.Getenv("BRIDGE_STAGING_SANDBOX_READER_GID"))
	if err != nil || root == "" || readerGID < 1 {
		t.Fatal("sandbox root and reader group are required")
	}
	if os.Geteuid() == 0 || os.Getegid() == readerGID || sandboxGroupMember(readerGID) {
		t.Fatal("writer must be unprivileged with a distinct primary group and no reader-group membership")
	}
	if caps, capErr := sandboxEffectiveCapabilities(); capErr != nil || caps != "0000000000000000" {
		t.Fatalf("writer capabilities must be empty: %q %v", caps, capErr)
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0750 || info.Mode()&os.ModeSetgid == 0 {
		t.Fatalf("root must be preprovisioned mode 2750: %v %v", info, err)
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(owner.Uid) != os.Geteuid() || int(owner.Gid) != readerGID {
		t.Fatal("root must be owned by the synthetic writer and reviewed reader group")
	}
	body := []byte("sandbox nested staging fixture")
	digest := sha256.Sum256(body)
	m := catalog.Model{ID: "sandbox-model", Repository: "Qwen/sandbox-model", Revision: strings.Repeat("a", 40), Files: []catalog.File{{Path: "nested/weights.safetensors", Size: int64(len(body)), Algorithm: "sha256", Digest: hex.EncodeToString(digest[:])}}}
	s, err := NewStager(root, 1<<30)
	if err != nil {
		t.Fatal(err)
	}
	var downloads atomic.Int32
	s.free = func(string) (int64, error) { return 2 << 30, nil }
	s.client = &http.Client{Transport: sandboxRoundTripper(func(*http.Request) (*http.Response, error) {
		downloads.Add(1)
		return &http.Response{StatusCode: http.StatusOK, ContentLength: int64(len(body)), Body: io.NopCloser(bytes.NewReader(body)), Header: http.Header{}}, nil
	})}
	legacy := m
	legacy.ID = "legacy-sandbox-model"
	legacy.Revision = strings.Repeat("f", 40)
	prepareSandboxLegacySnapshot(t, root, legacy, body, digest)
	if err = installRestrictSUIDSGIDFilter(); err != nil {
		t.Fatal(err)
	}
	control := root + "/.sandbox-chmod-control"
	if err = os.Mkdir(control, 0750); err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(control, os.ModeSetgid|0750); !errors.Is(err, syscall.EPERM) {
		t.Fatalf("seccomp filter permitted set-id chmod: %v", err)
	}
	if err = os.Chmod(control, 0750); err != nil {
		t.Fatalf("seccomp filter rejected ordinary publication chmod: %v", err)
	}
	controlFile, err := os.OpenFile(control+"/file", os.O_CREATE|os.O_WRONLY, 0640)
	if err != nil {
		t.Fatal(err)
	}
	if err = controlFile.Chmod(os.ModeSetgid | 0640); !errors.Is(err, syscall.EPERM) {
		controlFile.Close()
		t.Fatalf("seccomp filter permitted set-id fchmod: %v", err)
	}
	if err = controlFile.Chmod(0640); err != nil {
		controlFile.Close()
		t.Fatalf("seccomp filter rejected ordinary fchmod: %v", err)
	}
	if err = controlFile.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Verify(context.Background(), legacy); err != nil {
		t.Fatalf("legacy setgid snapshot verification failed: %v", err)
	}
	if _, err = s.Stage(context.Background(), legacy, nil); err != nil {
		t.Fatalf("legacy setgid snapshot repair failed: %v", err)
	}
	if downloads.Load() != 0 {
		t.Fatal("legacy permission repair downloaded a model", downloads.Load())
	}
	_, err = s.Stage(context.Background(), m, nil)
	if err != nil {
		t.Fatal(err)
	}
	if downloads.Load() != 1 {
		t.Fatal("unexpected download count", downloads.Load())
	}
	if _, err = s.Verify(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	if _, err = s.RepairPublishedPermissions(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	prefix, _ := modelPrefix(m)
	receiptPath := root + "/" + prefix + "/.bridge-receipt.json"
	receipt, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	receiptInfo, err := os.Stat(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chown(root+"/"+prefix+"/nested/weights.safetensors", os.Geteuid(), os.Getegid()); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Stage(context.Background(), m, nil); err == nil {
		t.Fatal("repeat staging accepted a published file outside the reviewed reader group")
	}
	if downloads.Load() != 1 {
		t.Fatal("wrong-group repair redownloaded a model", downloads.Load())
	}
	if after, readErr := os.ReadFile(receiptPath); readErr != nil || !bytes.Equal(after, receipt) {
		t.Fatalf("wrong-group repair changed receipt: %v", readErr)
	}
	if after, statErr := os.Stat(receiptPath); statErr != nil || after.Mode() != receiptInfo.Mode() {
		t.Fatalf("wrong-group repair changed receipt mode: %v", statErr)
	}
	// The intentionally corrupted revision above is retained as evidence that a
	// wrong reader group is not repaired implicitly. Stage another immutable
	// revision so the separate reader probe exercises a known-good publication.
	good := m
	good.Revision = strings.Repeat("c", 40)
	if _, err = s.Stage(context.Background(), good, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Verify(context.Background(), good); err != nil {
		t.Fatal(err)
	}
	// Publication failure after the verified snapshot is prepared must retain a
	// private partial and never create a ready destination.
	failed := m
	failed.Revision = strings.Repeat("e", 40)
	defaultRename := s.rename
	s.rename = func(*os.Root, string, string) error { return errors.New("injected publication failure") }
	if _, err = s.Stage(context.Background(), failed, nil); err == nil {
		t.Fatal("injected publication failure succeeded")
	}
	s.rename = defaultRename
	failedPrefix, _ := modelPrefix(failed)
	if _, statErr := os.Stat(root + "/" + failedPrefix); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("failed publication made a ready destination: %v", statErr)
	}
	partial := m
	partial.Revision = strings.Repeat("d", 40)
	if _, err = s.Stage(context.Background(), partial, func(string, int64) error { return context.Canceled }); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	assertSandboxPrivatePartial(t, root)
}

func prepareSandboxLegacySnapshot(t *testing.T, root string, m catalog.Model, body []byte, digest [sha256.Size]byte) {
	t.Helper()
	prefix, err := modelPrefix(m)
	if err != nil {
		t.Fatal(err)
	}
	previousUmask := syscall.Umask(0027)
	defer syscall.Umask(previousUmask)
	for _, dir := range []string{m.ID, prefix, prefix + "/nested"} {
		if err = os.Mkdir(root+"/"+dir, 0750); err != nil {
			t.Fatal(err)
		}
	}
	if err = os.WriteFile(root+"/"+prefix+"/nested/weights.safetensors", body, 0640); err != nil {
		t.Fatal(err)
	}
	receipt := stagedReceipt{
		Model:        m.ID,
		Revision:     m.Revision,
		ManifestHash: domain.Hash(m.Files),
		Status:       "verified-inputs-not-qualified",
		Files: []domain.Artifact{{
			Name: m.Files[0].Path, SHA256: hex.EncodeToString(digest[:]), Size: int64(len(body)), SourceRevision: m.Revision, Qualification: "staged-inputs-not-qualified",
		}},
		VerifiedAt: time.Now().UTC(),
	}
	encoded, err := json.Marshal(receipt)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(root+"/"+prefix+"/.bridge-receipt.json", encoded, 0640); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{m.ID, prefix, prefix + "/nested"} {
		info, statErr := os.Stat(root + "/" + dir)
		if statErr != nil || info.Mode().Perm() != 0750 || info.Mode()&os.ModeSetgid == 0 {
			t.Fatalf("legacy directory %s is not inherited 2750: %v %v", dir, info, statErr)
		}
	}
}

// TestStagingRestrictSUIDSGIDReaderProbe is invoked by the disposable
// container runner under the synthetic reader identity. It is intentionally a
// separate process from the writer, whose seccomp filter is irreversible.
func TestStagingRestrictSUIDSGIDReaderProbe(t *testing.T) {
	if os.Getenv("BRIDGE_STAGING_SANDBOX_READER_PROBE") != "1" {
		t.Skip("NOT RUN — invoked only by the isolated staging sandbox runner")
	}
	published := os.Getenv("BRIDGE_STAGING_SANDBOX_PUBLISHED")
	publishedReceipt := os.Getenv("BRIDGE_STAGING_SANDBOX_PUBLISHED_RECEIPT")
	partials := strings.Split(os.Getenv("BRIDGE_STAGING_SANDBOX_PARTIALS"), ":")
	partialReceipt := os.Getenv("BRIDGE_STAGING_SANDBOX_PARTIAL_RECEIPT")
	readerGID, err := strconv.Atoi(os.Getenv("BRIDGE_STAGING_SANDBOX_READER_GID"))
	if err != nil || published == "" || publishedReceipt == "" || partialReceipt == "" || len(partials) == 0 || partials[0] == "" || readerGID < 1 {
		t.Fatal("reader probe paths and reader group are required")
	}
	if os.Geteuid() == 0 || os.Getegid() != readerGID {
		t.Fatal("reader probe must run as the synthetic reader identity")
	}
	for _, p := range []string{published, publishedReceipt} {
		if _, err = os.ReadFile(p); err != nil {
			t.Fatalf("reader could not read published file %s: %v", p, err)
		}
		if f, openErr := os.OpenFile(p, os.O_WRONLY, 0); openErr == nil {
			f.Close()
			t.Fatal("reader unexpectedly opened published file for writing", p)
		} else if !errors.Is(openErr, syscall.EACCES) {
			t.Fatalf("reader publication write denial was not EACCES: %v", openErr)
		}
	}
	for _, partial := range partials {
		if _, err = os.ReadFile(partial); !errors.Is(err, syscall.EACCES) {
			t.Fatalf("reader private partial denial for %s was not EACCES: %v", partial, err)
		}
	}
	if _, err = os.ReadFile(partialReceipt); !errors.Is(err, syscall.EACCES) {
		t.Fatalf("reader partial receipt denial was not EACCES: %v", err)
	}
}

type sandboxRoundTripper func(*http.Request) (*http.Response, error)

func (f sandboxRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func sandboxGroupMember(wanted int) bool {
	groups, err := os.Getgroups()
	if err != nil {
		return false
	}
	for _, group := range groups {
		if group == wanted {
			return true
		}
	}
	return false
}

func assertSandboxPrivatePartial(t *testing.T, root string) {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), ".partial-") {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
			t.Fatalf("partial %s is not owner-only: %v %v", entry.Name(), info, err)
		}
		if _, err = os.Stat(root + "/" + entry.Name() + "/snapshot/nested/weights.safetensors"); err != nil {
			t.Fatalf("partial %s lacks a retained nested fixture: %v", entry.Name(), err)
		}
		count++
	}
	if count < 2 {
		t.Fatalf("expected publication-failure and cancellation partials, got %d", count)
	}
}

func sandboxEffectiveCapabilities() (string, error) {
	b, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(b), "\n") {
		if value, ok := strings.CutPrefix(line, "CapEff:\t"); ok {
			return value, nil
		}
	}
	return "", errors.New("CapEff missing")
}
