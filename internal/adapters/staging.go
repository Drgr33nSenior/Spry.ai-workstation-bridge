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
}
type stagedReceipt struct {
	Model        string            `json:"model"`
	Revision     string            `json:"revision"`
	ManifestHash string            `json:"manifest_sha256"`
	Status       string            `json:"status"`
	Files        []domain.Artifact `json:"files"`
	VerifiedAt   time.Time         `json:"verified_at"`
}

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
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode()&os.ModeSetgid == 0 || info.Mode().Perm()&0022 != 0 {
		return nil, errors.New("model root must be a real setgid directory with the reviewed workload reader group and no group/world write access")
	}
	return &Stager{root: root, budget: budget, client: downloadClient(), free: freeBytes}, nil
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
		return nil, errors.New("model revision already exists; verify it explicitly")
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
	var artifacts []domain.Artifact
	var copied int64
	for _, f := range m.Files {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		rel := tmpName + "/" + f.Path
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
	file, e := root.OpenFile(tmpName+"/.bridge-receipt.json", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0640)
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
	dir, e := root.Open(tmpName)
	if e != nil {
		return nil, e
	}
	e = dir.Sync()
	dir.Close()
	if e != nil {
		return nil, e
	}
	if e = rootParents(root, prefix); e != nil {
		return nil, e
	}
	// Until every file is verified, the partial snapshot is owner-only. Its GID
	// and every file GID were inherited from the administrator-set model root.
	if e = root.Chmod(tmpName, 0750); e != nil {
		return nil, e
	}
	if _, e = root.Lstat(prefix); e == nil {
		return nil, errors.New("model publication conflict")
	}
	if e = root.Rename(tmpName, prefix); e != nil {
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
		i, e := root.Lstat(prefix + "/" + f.Path)
		if e != nil || !i.Mode().IsRegular() || i.Size() != f.Size {
			return nil, errors.New("model file missing, symlinked or wrong size")
		}
		in, e := root.Open(prefix + "/" + f.Path)
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
	err := fs.WalkDir(root.FS(), prefix, func(p string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.Type()&os.ModeSymlink != 0 {
			return errors.New("staged model contains symlink")
		}
		if !d.IsDir() && !expected[strings.TrimPrefix(p, prefix+"/")] {
			return errors.New("staged model contains unreviewed extra file")
		}
		return nil
	})
	return result, err
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
	if json.NewDecoder(io.LimitReader(f, 1<<20)).Decode(&receipt) != nil || receipt.ManifestHash != domain.Hash(m.Files) || receipt.Revision != m.Revision {
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
