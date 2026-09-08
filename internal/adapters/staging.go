package adapters

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/catalog"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/domain"
)

type Stager struct {
	root   string
	budget int64
	client *http.Client
	mu     sync.Mutex
	free   func(string) (int64, error)
	rename func(*os.Root, string, string) error
}
type stagedReceipt struct {
	Model        string            `json:"model"`
	Revision     string            `json:"revision"`
	ManifestHash string            `json:"manifest_sha256"`
	Status       string            `json:"status"`
	Files        []domain.Artifact `json:"files"`
	VerifiedAt   time.Time         `json:"verified_at"`
}

const (
	readerDirectoryMode fs.FileMode = os.ModeSetgid | 0750
	readerFileMode      fs.FileMode = 0640
	// Keep setgid while denying all group traversal: children must inherit the
	// reviewed reader group even though partial data remains owner-only.
	privateDirectoryMode fs.FileMode = os.ModeSetgid | 0700
)

var origins = map[string]bool{"huggingface.co": true, "cdn-lfs.huggingface.co": true, "cdn-lfs.hf.co": true, "cdn-lfs-us-1.hf.co": true, "cas-bridge.xethub.hf.co": true}

type downloadProgress struct {
	callback    func(string, int64) error
	name        string
	base, total int64
	last        time.Time
}

func (p *downloadProgress) Write(b []byte) (int, error) {
	p.total += int64(len(b))
	if p.callback != nil && time.Since(p.last) >= time.Second {
		p.last = time.Now()
		if e := p.callback(p.name, p.base+p.total); e != nil {
			return 0, e
		}
	}
	return len(b), nil
}

func publicAddress(ip net.IP) bool {
	a, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	a = a.Unmap()
	if !a.IsGlobalUnicast() || a.IsPrivate() || a.IsLoopback() || a.IsLinkLocalUnicast() {
		return false
	}
	for _, cidr := range []string{"0.0.0.0/8", "100.64.0.0/10", "169.254.0.0/16", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "2001:db8::/32"} {
		if netip.MustParsePrefix(cidr).Contains(a) {
			return false
		}
	}
	return true
}
func reviewedURL(u *url.URL) bool {
	return u.Scheme == "https" && u.User == nil && origins[strings.ToLower(u.Hostname())] && (u.Port() == "" || u.Port() == "443") && u.Fragment == ""
}
func downloadClient() *http.Client {
	dialer := &net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}, TLSHandshakeTimeout: 15 * time.Second, ResponseHeaderTimeout: 30 * time.Second, DisableCompression: true, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, e := net.SplitHostPort(address)
		if e != nil {
			return nil, errors.New("invalid download destination")
		}
		ips, e := net.DefaultResolver.LookupIP(ctx, "ip", host)
		if e != nil {
			return nil, errors.New("download DNS resolution failed")
		}
		if len(ips) == 0 {
			return nil, errors.New("download host has no address")
		}
		for _, ip := range ips {
			if !publicAddress(ip) {
				return nil, errors.New("download destination is not a public address")
			}
		}
		for _, ip := range ips {
			c, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if err == nil {
				return c, nil
			}
		}
		return nil, errors.New("download connection failed")
	}}
	return &http.Client{Transport: transport, Timeout: 30 * time.Minute, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) > 5 || !reviewedURL(r.URL) {
			return errors.New("download redirect is outside reviewed HTTPS origins")
		}
		r.Header.Del("Authorization")
		r.Header.Del("Cookie")
		r.Header.Del("Proxy-Authorization")
		return nil
	}}
}
func NewStager(root string, budget int64) (*Stager, error) {
	if !filepath.IsAbs(root) || root == "/" || budget < 1 {
		return nil, errors.New("managed model root and byte budget are required")
	}
	info, e := os.Lstat(root)
	if e != nil {
		return nil, e
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode()&os.ModeSetgid == 0 || info.Mode().Perm() != 0750 {
		return nil, errors.New("model root must be a real 2750 setgid directory with the reviewed workload reader group")
	}
	return &Stager{root: root, budget: budget, client: downloadClient(), free: freeBytes, rename: func(r *os.Root, oldName, newName string) error {
		return r.Rename(oldName, newName)
	}}, nil
}
func safeRelative(p string) bool {
	return p != "" && path.Clean(p) == p && !strings.HasPrefix(p, "/") && !strings.Contains(p, "\\") && !strings.Contains(p, "\x00") && p != ".." && !strings.HasPrefix(p, "../")
}
func rootParents(root *os.Root, p string) error {
	rootInfo, e := root.Stat(".")
	if e != nil {
		return e
	}
	rootOwner, ok := rootInfo.Sys().(*syscall.Stat_t)
	if !ok {
		return errors.New("model root group cannot be observed")
	}
	parts := strings.Split(path.Dir(p), "/")
	cur := ""
	for _, part := range parts {
		if part == "." {
			continue
		}
		cur = path.Join(cur, part)
		if e := root.Mkdir(cur, 0750); e != nil && !errors.Is(e, fs.ErrExist) {
			return e
		}
		i, e := root.Lstat(cur)
		if e != nil {
			return e
		}
		if !i.IsDir() || i.Mode()&os.ModeSymlink != 0 {
			return errors.New("managed path contains a symlink or non-directory")
		}
		owner, ok := i.Sys().(*syscall.Stat_t)
		if !ok || owner.Gid != rootOwner.Gid {
			return errors.New("model directory does not retain the reviewed reader group")
		}
	}
	return nil
}

func rootGroup(root *os.Root) (uint32, error) {
	i, e := root.Stat(".")
	if e != nil {
		return 0, e
	}
	owner, ok := i.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, errors.New("model root group cannot be observed")
	}
	return owner.Gid, nil
}

func checkedDirectory(root *os.Root, p string, group uint32) (fs.FileInfo, error) {
	i, e := root.Lstat(p)
	if e != nil {
		return nil, e
	}
	if !i.IsDir() || i.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("managed path contains a symlink or non-directory")
	}
	owner, ok := i.Sys().(*syscall.Stat_t)
	if !ok || owner.Gid != group {
		return nil, errors.New("model directory does not retain the reviewed reader group")
	}
	return i, nil
}

func checkedRegularFile(root *os.Root, p string, group uint32) (fs.FileInfo, error) {
	i, e := root.Lstat(p)
	if e != nil {
		return nil, e
	}
	if !i.Mode().IsRegular() || i.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("model file is missing, symlinked or not regular")
	}
	owner, ok := i.Sys().(*syscall.Stat_t)
	if !ok || owner.Gid != group {
		return nil, errors.New("model file does not retain the reviewed reader group")
	}
	return i, nil
}

func requireReaderDirectory(root *os.Root, p string, group uint32) error {
	i, e := checkedDirectory(root, p, group)
	if e != nil {
		return e
	}
	if i.Mode().Perm() != 0750 || i.Mode()&os.ModeSetgid == 0 {
		return errors.New("published model directory is not group-traversable setgid mode 2750")
	}
	return nil
}

func requireReaderFile(root *os.Root, p string, group uint32) error {
	i, e := checkedRegularFile(root, p, group)
	if e != nil {
		return e
	}
	if i.Mode().Perm() != readerFileMode || i.Mode()&(os.ModeSetgid|os.ModeSticky) != 0 {
		return errors.New("published model file is not group-readable mode 0640")
	}
	return nil
}

func snapshotDirectories(m catalog.Model) []string {
	dirs := map[string]bool{"": true}
	for _, f := range m.Files {
		for dir := path.Dir(f.Path); dir != "."; dir = path.Dir(dir) {
			dirs[dir] = true
		}
	}
	result := make([]string, 0, len(dirs))
	for dir := range dirs {
		result = append(result, dir)
	}
	// Parents must be chmodded before children. Lexical order provides that for
	// reviewed slash-separated relative paths.
	slices.Sort(result)
	return result
}

func joinSnapshot(snapshot, rel string) string {
	if rel == "" {
		return snapshot
	}
	return snapshot + "/" + rel
}

func receiptMatches(receipt stagedReceipt, m catalog.Model, artifacts []domain.Artifact) bool {
	if receipt.Model != m.ID || receipt.Revision != m.Revision || receipt.ManifestHash != domain.Hash(m.Files) || receipt.Status != "verified-inputs-not-qualified" || receipt.VerifiedAt.IsZero() || len(receipt.Files) != len(artifacts) {
		return false
	}
	for i := range artifacts {
		// Receipts preserve the staging qualification. A later explicit verify
		// reports the stronger "verified" observation to its caller.
		want := artifacts[i]
		want.Qualification = "staged-inputs-not-qualified"
		if receipt.Files[i] != want {
			return false
		}
	}
	return true
}

// validateSnapshot proves the reviewed manifest, receipt and exact managed tree.
// It deliberately separates evidence validation from publication permissions so
// a verified older snapshot can be repaired without trusting its previous mode.
func validateSnapshot(ctx context.Context, root *os.Root, snapshot string, m catalog.Model, requireModes bool) ([]domain.Artifact, error) {
	group, e := rootGroup(root)
	if e != nil {
		return nil, e
	}
	directories := snapshotDirectories(m)
	for _, dir := range directories {
		p := joinSnapshot(snapshot, dir)
		if requireModes {
			if e = requireReaderDirectory(root, p, group); e != nil {
				return nil, e
			}
		} else if _, e = checkedDirectory(root, p, group); e != nil {
			return nil, e
		}
	}

	var result []domain.Artifact
	expected := map[string]bool{".bridge-receipt.json": true}
	for _, f := range m.Files {
		if !safeRelative(f.Path) {
			return nil, errors.New("unsafe manifest path")
		}
		expected[f.Path] = true
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		p := snapshot + "/" + f.Path
		i, e := checkedRegularFile(root, p, group)
		if e != nil || i.Size() != f.Size {
			return nil, errors.New("model file missing, symlinked or wrong size")
		}
		if requireModes {
			if e = requireReaderFile(root, p, group); e != nil {
				return nil, e
			}
		}
		in, e := root.Open(p)
		if e != nil {
			return nil, e
		}
		h, e := hashFor(f)
		if e != nil {
			in.Close()
			return nil, e
		}
		sh := sha256.New()
		_, e = io.Copy(io.MultiWriter(h, sh), io.LimitReader(in, f.Size+1))
		in.Close()
		if e != nil || hex.EncodeToString(h.Sum(nil)) != f.Digest {
			return nil, errors.New("staged model digest mismatch")
		}
		result = append(result, domain.Artifact{Name: f.Path, SHA256: hex.EncodeToString(sh.Sum(nil)), Size: f.Size, SourceRevision: m.Revision, Qualification: "verified-inputs-not-qualified"})
	}

	receiptPath := snapshot + "/.bridge-receipt.json"
	if requireModes {
		if e = requireReaderFile(root, receiptPath, group); e != nil {
			return nil, e
		}
	} else if _, e = checkedRegularFile(root, receiptPath, group); e != nil {
		return nil, e
	}
	f, e := root.Open(receiptPath)
	if e != nil {
		return nil, e
	}
	var receipt stagedReceipt
	decodeErr := json.NewDecoder(io.LimitReader(f, 1<<20)).Decode(&receipt)
	closeErr := f.Close()
	if decodeErr != nil || closeErr != nil || !receiptMatches(receipt, m, result) {
		return nil, errors.New("staged model receipt does not match verified manifest")
	}

	err := fs.WalkDir(root.FS(), snapshot, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.Type()&os.ModeSymlink != 0 {
			return errors.New("staged model contains symlink")
		}
		rel := strings.TrimPrefix(p, snapshot)
		rel = strings.TrimPrefix(rel, "/")
		if d.IsDir() {
			if !containsSnapshotDirectory(directories, rel) {
				return errors.New("staged model contains unreviewed directory")
			}
			return nil
		}
		if !d.Type().IsRegular() || !expected[rel] {
			return errors.New("staged model contains unreviewed extra file")
		}
		return nil
	})
	return result, err
}

func containsSnapshotDirectory(directories []string, wanted string) bool {
	for _, dir := range directories {
		if dir == wanted {
			return true
		}
	}
	return false
}

func setPublishedSnapshotPermissions(root *os.Root, snapshot string, m catalog.Model) error {
	group, e := rootGroup(root)
	if e != nil {
		return e
	}
	for _, dir := range snapshotDirectories(m) {
		p := joinSnapshot(snapshot, dir)
		if _, e = checkedDirectory(root, p, group); e != nil {
			return e
		}
		if e = root.Chmod(p, readerDirectoryMode); e != nil {
			return e
		}
		if e = requireReaderDirectory(root, p, group); e != nil {
			return e
		}
	}
	for _, f := range append(append([]catalog.File(nil), m.Files...), catalog.File{Path: ".bridge-receipt.json"}) {
		p := snapshot + "/" + f.Path
		if _, e = checkedRegularFile(root, p, group); e != nil {
			return e
		}
		if e = root.Chmod(p, readerFileMode); e != nil {
			return e
		}
		if e = requireReaderFile(root, p, group); e != nil {
			return e
		}
	}
	return nil
}

func setModelParentPermissions(root *os.Root, prefix string) error {
	parent := path.Dir(prefix)
	group, e := rootGroup(root)
	if e != nil {
		return e
	}
	if _, e = checkedDirectory(root, parent, group); e != nil {
		return e
	}
	if e = root.Chmod(parent, readerDirectoryMode); e != nil {
		return e
	}
	return requireReaderDirectory(root, parent, group)
}

func publishedReaderModes(root *os.Root, prefix string, m catalog.Model) error {
	group, e := rootGroup(root)
	if e != nil {
		return e
	}
	if e = requireReaderDirectory(root, path.Dir(prefix), group); e != nil {
		return e
	}
	for _, dir := range snapshotDirectories(m) {
		if e = requireReaderDirectory(root, joinSnapshot(prefix, dir), group); e != nil {
			return e
		}
	}
	for _, f := range append(append([]catalog.File(nil), m.Files...), catalog.File{Path: ".bridge-receipt.json"}) {
		if e = requireReaderFile(root, prefix+"/"+f.Path, group); e != nil {
			return e
		}
	}
	return nil
}
func hashFor(f catalog.File) (hash.Hash, error) {
	switch f.Algorithm {
	case "sha256":
		if len(f.Digest) != 64 {
			return nil, errors.New("invalid SHA256")
		}
		return sha256.New(), nil
	case "git-sha1":
		if len(f.Digest) != 40 {
			return nil, errors.New("invalid Git blob hash")
		}
		h := sha1.New()
		fmt.Fprintf(h, "blob %d%c", f.Size, 0)
		return h, nil
	default:
		return nil, errors.New("unsupported artifact digest")
	}
}
func modelPrefix(m catalog.Model) (string, error) {
	if !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,100}$`).MatchString(m.ID) || !regexp.MustCompile(`^[a-f0-9]{40}$`).MatchString(m.Revision) {
		return "", errors.New("invalid model identity")
	}
	return m.ID + "/" + m.Revision, nil
}
func usedBytes(root *os.Root) (int64, error) {
	var total int64
	err := fs.WalkDir(root.FS(), ".", func(p string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.Type()&os.ModeSymlink != 0 {
			return errors.New("managed tree contains symlink")
		}
		if !d.IsDir() {
			i, e := d.Info()
			if e != nil {
				return e
			}
			if !i.Mode().IsRegular() {
				return errors.New("managed tree contains special file")
			}
			total += i.Size()
		}
		return nil
	})
	return total, err
}

func (s *Stager) Stage(ctx context.Context, m catalog.Model, progress func(string, int64) error) ([]domain.Artifact, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	root, e := os.OpenRoot(s.root)
	if e != nil {
		return nil, e
	}
	defer root.Close()
	prefix, e := modelPrefix(m)
	if e != nil {
		return nil, e
	}
	if _, e = root.Lstat(prefix); e == nil {
		// A repeat stage is the explicit repair workflow for a published legacy
		// snapshot. It still refuses malformed receipts, hashes and paths rather
		// than treating an existing directory as ready.
		return repairPublishedPermissions(ctx, root, prefix, m)
	}
	var needed int64
	seen := map[string]bool{}
	for _, f := range m.Files {
		if !safeRelative(f.Path) || seen[f.Path] || f.Size < 0 {
			return nil, errors.New("unsafe or duplicate model file")
		}
		seen[f.Path] = true
		if _, e = hashFor(f); e != nil {
			return nil, e
		}
		needed += f.Size
		if needed > s.budget {
			return nil, errors.New("model snapshot exceeds budget")
		}
	}
	used, e := usedBytes(root)
	if e != nil {
		return nil, e
	}
	free, e := s.free(s.root)
	if e != nil {
		return nil, e
	}
	if needed > s.budget-used || needed+256*1024*1024 > free {
		return nil, errors.New("insufficient managed storage budget or free space")
	}
	tmp, e := os.MkdirTemp(s.root, ".partial-")
	if e != nil {
		return nil, e
	}
	tmpName := filepath.Base(tmp)
	// MkdirTemp is owner-only, but make that invariant explicit rather than
	// depending on either its implementation or the service umask. The child
	// snapshot can receive reader modes while this parent keeps it private until
	// the atomic rename into the published model-ID directory.
	if e = root.Chmod(tmpName, privateDirectoryMode); e != nil {
		return nil, e
	}
	if i, privateErr := root.Lstat(tmpName); privateErr != nil || !i.IsDir() || i.Mode().Perm() != 0700 || i.Mode()&os.ModeSetgid == 0 {
		return nil, errors.New("partial model staging directory is not owner-only")
	}
	snapshot := tmpName + "/snapshot"
	if e = root.Mkdir(snapshot, 0700); e != nil {
		return nil, e
	}
	if e = root.Chmod(snapshot, privateDirectoryMode); e != nil {
		return nil, e
	}
	var artifacts []domain.Artifact
	var copied int64
	for _, f := range m.Files {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		rel := snapshot + "/" + f.Path
		if e = rootParents(root, rel); e != nil {
			return nil, e
		}
		u := &url.URL{Scheme: "https", Host: "huggingface.co", Path: "/" + m.Repository + "/resolve/" + m.Revision + "/" + f.Path}
		if !reviewedURL(u) {
			return nil, errors.New("model origin not reviewed")
		}
		req, e := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if e != nil {
			return nil, errors.New("invalid model request")
		}
		response, e := s.client.Do(req)
		if e != nil {
			return nil, errors.New("model download failed; partial files retained without publication")
		}
		if response.StatusCode != http.StatusOK {
			response.Body.Close()
			return nil, fmt.Errorf("model download HTTP status %d", response.StatusCode)
		}
		if response.ContentLength >= 0 && response.ContentLength != f.Size {
			response.Body.Close()
			return nil, errors.New("upstream file size differs from reviewed manifest")
		}
		out, e := root.OpenFile(rel, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0640)
		if e != nil {
			response.Body.Close()
			return nil, e
		}
		fileInfo, fileErr := out.Stat()
		rootInfo, rootErr := root.Stat(".")
		if fileErr != nil || rootErr != nil {
			out.Close()
			response.Body.Close()
			return nil, errors.New("cannot verify model reader group")
		}
		fileOwner, fok := fileInfo.Sys().(*syscall.Stat_t)
		rootOwner, rok := rootInfo.Sys().(*syscall.Stat_t)
		if !fok || !rok || fileOwner.Gid != rootOwner.Gid {
			out.Close()
			response.Body.Close()
			return nil, errors.New("model file did not inherit reviewed reader group")
		}
		h, _ := hashFor(f)
		sh := sha256.New()
		n, copyErr := io.Copy(io.MultiWriter(out, h, sh, &downloadProgress{callback: progress, name: f.Path, base: copied, last: time.Now()}), io.LimitReader(response.Body, f.Size+1))
		closeErr := response.Body.Close()
		syncErr := out.Sync()
		fileErr = out.Close()
		if copyErr != nil || closeErr != nil || syncErr != nil || fileErr != nil || n != f.Size {
			return nil, errors.New("interrupted or incomplete model file; partial retained")
		}
		if hex.EncodeToString(h.Sum(nil)) != f.Digest {
			return nil, errors.New("model integrity check failed; partial retained")
		}
		copied += n
		artifacts = append(artifacts, domain.Artifact{Name: f.Path, SHA256: hex.EncodeToString(sh.Sum(nil)), Size: n, SourceRevision: m.Revision, Qualification: "staged-inputs-not-qualified"})
		if progress != nil {
			if e = progress(f.Path, copied); e != nil {
				return nil, e
			}
		}
	}
	receipt := stagedReceipt{Model: m.ID, Revision: m.Revision, ManifestHash: domain.Hash(m.Files), Status: "verified-inputs-not-qualified", Files: artifacts, VerifiedAt: time.Now().UTC()}
	b, _ := json.MarshalIndent(receipt, "", "  ")
	file, e := root.OpenFile(snapshot+"/.bridge-receipt.json", os.O_WRONLY|os.O_CREATE|os.O_EXCL, readerFileMode)
	if e != nil {
		return nil, e
	}
	_, e = file.Write(b)
	if e == nil {
		e = file.Sync()
	}
	closeErr := file.Close()
	if e != nil {
		return nil, e
	}
	if closeErr != nil {
		return nil, closeErr
	}
	dir, e := root.Open(snapshot)
	if e != nil {
		return nil, e
	}
	e = dir.Sync()
	dir.Close()
	if e != nil {
		return nil, e
	}
	// A model ID parent is a single reviewed path. It may have been made under
	// the 0077 service umask by an older run, so establish its exact traversal
	// mode before its verified child is published.
	if e = rootParents(root, prefix); e != nil {
		return nil, e
	}
	if e = setModelParentPermissions(root, prefix); e != nil {
		return nil, e
	}
	if _, e = validateSnapshot(ctx, root, snapshot, m, false); e != nil {
		return nil, e
	}
	// The snapshot remains unreachable to workload readers through its private
	// .partial-* parent while these explicit modes defeat the service umask.
	if e = setPublishedSnapshotPermissions(root, snapshot, m); e != nil {
		return nil, e
	}
	if i, privateErr := root.Lstat(tmpName); privateErr != nil || !i.IsDir() || i.Mode().Perm() != 0700 || i.Mode()&os.ModeSetgid == 0 {
		return nil, errors.New("partial model staging directory lost owner-only protection")
	}
	if _, e = root.Lstat(prefix); e == nil {
		return nil, errors.New("model publication conflict")
	}
	if e = s.rename(root, snapshot, prefix); e != nil {
		return nil, e
	}
	dir, e = root.Open(path.Dir(prefix))
	if e != nil {
		return nil, e
	}
	defer dir.Close()
	if e = dir.Sync(); e != nil {
		return nil, e
	}
	if e = root.Remove(tmpName); e != nil {
		return nil, errors.New("published model snapshot but could not remove private staging directory")
	}
	rootDir, e := root.Open(".")
	if e != nil {
		return nil, e
	}
	e = rootDir.Sync()
	rootDir.Close()
	if e != nil {
		return nil, e
	}
	if _, e = validateSnapshot(ctx, root, prefix, m, true); e != nil {
		return nil, e
	}
	return artifacts, nil
}

func (s *Stager) Verify(ctx context.Context, m catalog.Model) ([]domain.Artifact, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	root, e := os.OpenRoot(s.root)
	if e != nil {
		return nil, e
	}
	defer root.Close()
	prefix, e := modelPrefix(m)
	if e != nil {
		return nil, e
	}
	if e = publishedReaderModes(root, prefix, m); e != nil {
		return nil, e
	}
	return validateSnapshot(ctx, root, prefix, m, true)
}

// RepairPublishedPermissions repairs only a snapshot that still proves its
// reviewed manifest and receipt. It neither traverses arbitrary paths nor makes
// incomplete .partial-* downloads reader-accessible.
func (s *Stager) RepairPublishedPermissions(ctx context.Context, m catalog.Model) ([]domain.Artifact, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	root, e := os.OpenRoot(s.root)
	if e != nil {
		return nil, e
	}
	defer root.Close()
	prefix, e := modelPrefix(m)
	if e != nil {
		return nil, e
	}
	return repairPublishedPermissions(ctx, root, prefix, m)
}

func repairPublishedPermissions(ctx context.Context, root *os.Root, prefix string, m catalog.Model) ([]domain.Artifact, error) {
	result, e := validateSnapshot(ctx, root, prefix, m, false)
	if e != nil {
		return nil, e
	}
	if e = setModelParentPermissions(root, prefix); e != nil {
		return nil, e
	}
	if e = setPublishedSnapshotPermissions(root, prefix, m); e != nil {
		return nil, e
	}
	if _, e = validateSnapshot(ctx, root, prefix, m, true); e != nil {
		return nil, e
	}
	return result, nil
}

// VerifyContent proves the receipt, reviewed manifest and exact managed tree
// without accepting publication permissions. Recovery inspection uses it only
// to describe a permission fault; it never chmods a snapshot implicitly.
func (s *Stager) VerifyContent(ctx context.Context, m catalog.Model) ([]domain.Artifact, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	root, e := os.OpenRoot(s.root)
	if e != nil {
		return nil, e
	}
	defer root.Close()
	prefix, e := modelPrefix(m)
	if e != nil {
		return nil, e
	}
	return validateSnapshot(ctx, root, prefix, m, false)
}

func (s *Stager) snapshotPresent(m catalog.Model) (bool, error) {
	prefix, e := modelPrefix(m)
	if e != nil {
		return false, e
	}
	root, e := os.OpenRoot(s.root)
	if e != nil {
		return false, e
	}
	defer root.Close()
	_, e = root.Lstat(prefix)
	if errors.Is(e, fs.ErrNotExist) {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	return true, nil
}

func (s *Stager) Status(m catalog.Model) string {
	p, e := modelPrefix(m)
	if e != nil {
		return "invalid"
	}
	root, e := os.OpenRoot(s.root)
	if e != nil {
		return "unavailable"
	}
	defer root.Close()
	f, e := root.Open(p + "/.bridge-receipt.json")
	if e != nil {
		return "not-staged"
	}
	defer f.Close()
	var receipt stagedReceipt
	if json.NewDecoder(io.LimitReader(f, 1<<20)).Decode(&receipt) != nil || receipt.ManifestHash != domain.Hash(m.Files) || receipt.Revision != m.Revision || publishedReaderModes(root, p, m) != nil {
		return "verification-required"
	}
	return receipt.Status + " at " + receipt.VerifiedAt.Format(time.RFC3339)
}

func CacheUsage(name, rootPath string, budget int64) domain.Cache {
	c := domain.Cache{Name: name, BudgetBytes: budget, Status: "unknown", CleanupPreview: []string{}}
	r, e := os.OpenRoot(rootPath)
	if e != nil {
		c.Status = "unavailable"
		return c
	}
	defer r.Close()
	n, e := usedBytes(r)
	if e != nil {
		c.Status = "unsafe-path"
		return c
	}
	c.UsedBytes = n
	c.Status = "observed"
	entries, e := fs.ReadDir(r.FS(), ".")
	if e == nil {
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), ".partial-") {
				c.CleanupPreview = append(c.CleanupPreview, entry.Name()+" — incomplete transfer; owner review only")
			}
		}
	}
	return c
}
