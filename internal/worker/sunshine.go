package worker

import (
	"archive/tar"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

//go:embed sunshine.Containerfile
var sunshineContainerfile []byte

const steamBaseDigest = "f6bd0f5d88c6a5160fe765f61af9b33702b01cde45717b89f9ff390f104882dd"

// verifyOCIBase accepts an OCI archive whose index points directly at the
// reviewed linux/amd64 manifest. It verifies the bytes of every SHA256 blob.
// It does not pull a registry image or substitute an OCI manifest conversion.
func verifyOCIBase(input io.Reader) error {
	return verifyOCIArchive(input, steamBaseDigest)
}
func verifyOCIArchive(input io.Reader, expectedManifest string) error {
	reader := tar.NewReader(input)
	hasIndex, hasManifest := false, false
	seen := map[string]bool{}
	for {
		h, e := reader.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			return errors.New("invalid base OCI archive")
		}
		name := strings.TrimPrefix(h.Name, "./")
		if h.Typeflag == tar.TypeDir {
			continue
		}
		if h.Typeflag != tar.TypeReg || name == "" || filepath.IsAbs(name) || strings.Contains(name, "..") || seen[name] || h.Size < 0 || h.Size > 32<<30 {
			return errors.New("unsafe base OCI archive entry")
		}
		seen[name] = true
		switch {
		case name == "index.json":
			if h.Size > 1<<20 {
				return errors.New("oversized OCI index")
			}
			var index struct {
				Manifests []struct {
					Digest string `json:"digest"`
				} `json:"manifests"`
			}
			if json.NewDecoder(io.LimitReader(reader, 1<<20)).Decode(&index) != nil {
				return errors.New("invalid OCI index")
			}
			for _, m := range index.Manifests {
				if m.Digest == "sha256:"+expectedManifest {
					hasIndex = true
				}
			}
		case strings.HasPrefix(name, "blobs/sha256/"):
			digest := strings.TrimPrefix(name, "blobs/sha256/")
			hash := sha256.New()
			if _, e = io.Copy(hash, reader); e != nil {
				return e
			}
			if len(digest) != 64 || hex.EncodeToString(hash.Sum(nil)) != digest {
				return errors.New("OCI blob digest mismatch")
			}
			if digest == expectedManifest {
				hasManifest = true
			}
		case name == "oci-layout":
		default:
			return errors.New("unexpected OCI base archive entry")
		}
	}
	if !hasIndex || !hasManifest {
		return errors.New("OCI base does not retain the exact reviewed Steam manifest digest")
	}
	return nil
}
func sunshineCommands(p Policy, job string) ([][]string, error) {
	f, e := os.Open(filepath.Join(p.SourceRoot, "base.oci"))
	if e != nil {
		return nil, errors.New("reviewed offline Steam base archive is missing")
	}
	e = verifyOCIBase(f)
	if e != nil {
		f.Close()
		return nil, e
	}
	if _, e = f.Seek(0, 0); e != nil {
		f.Close()
		return nil, e
	}
	configID, e := ociConfigID(f, steamBaseDigest)
	f.Close()
	if e != nil {
		return nil, e
	}
	deb, e := os.Open(filepath.Join(p.SourceRoot, "sunshine.deb"))
	if e != nil {
		return nil, e
	}
	hash := sha256.New()
	_, e = io.Copy(hash, deb)
	deb.Close()
	if e != nil || hex.EncodeToString(hash.Sum(nil)) != "c87f226920ad83055a898be1f0c7540307593e92d8b0baf1f076909758db8ca0" {
		return nil, errors.New("Sunshine package differs from reviewed release")
	}
	for _, dir := range []string{"apt/lists", "apt/archives", "infrastructure/gaming", "bin", "lib", "templates"} {
		info, e := os.Stat(filepath.Join(p.SourceRoot, dir))
		if e != nil || !info.IsDir() {
			return nil, errors.New("offline Sunshine package closure/context is incomplete")
		}
	}
	if e = os.WriteFile(filepath.Join(job, "Containerfile"), sunshineContainerfile, 0400); e != nil {
		return nil, e
	}
	base := []string{"/usr/bin/buildah", "--storage-driver=vfs", "--root=/build/storage", "--runroot=/build/runroot"}
	commands := [][]string{}
	add := func(args ...string) { commands = append(commands, append(append([]string{}, base...), args...)) }
	add("pull", "--quiet", "oci-archive:/source/base.oci")
	add("bud", "--isolation=chroot", "--pull=never", "--network=none", "--format=oci", "--build-arg", "BASE_IMAGE="+configID, "--file", "/build/Containerfile", "--tag", "localhost/bridge-sunshine:candidate", "/source")
	add("push", "localhost/bridge-sunshine:candidate", "oci-archive:/build/sunshine.oci")
	return commands, nil
}
func ociConfigID(input io.Reader, manifest string) (string, error) {
	r := tar.NewReader(input)
	for {
		h, e := r.Next()
		if e != nil {
			return "", errors.New("base OCI manifest configuration unavailable")
		}
		if strings.TrimPrefix(h.Name, "./") != "blobs/sha256/"+manifest {
			continue
		}
		if h.Size > 4<<20 {
			return "", errors.New("base OCI manifest too large")
		}
		var m struct {
			Config struct {
				Digest string `json:"digest"`
			} `json:"config"`
		}
		if json.NewDecoder(io.LimitReader(r, 4<<20)).Decode(&m) != nil || !strings.HasPrefix(m.Config.Digest, "sha256:") || len(m.Config.Digest) != 71 {
			return "", errors.New("base OCI configuration identity invalid")
		}
		if _, e = hex.DecodeString(strings.TrimPrefix(m.Config.Digest, "sha256:")); e != nil {
			return "", errors.New("base OCI configuration digest invalid")
		}
		return m.Config.Digest, nil
	}
}
