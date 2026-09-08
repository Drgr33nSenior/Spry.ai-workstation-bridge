// bridge-package creates local installation archives. It installs/publishes nothing.
package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	for _, target := range []string{"linux-amd64", "darwin-arm64", "darwin-amd64"} {
		files := map[string]string{}
		roots := []string{"dist/" + target, "deployment", "docs", "api"}
		for _, root := range roots {
			err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if d.IsDir() {
					return nil
				}
				if d.Type()&os.ModeSymlink != 0 {
					return fmt.Errorf("package source symlink refused")
				}
				rel := path
				if strings.HasPrefix(path, "dist/") {
					rel = "bin/" + filepath.Base(path)
				}
				files[rel] = path
				return nil
			})
			if err != nil {
				return err
			}
		}
		files["README.md"] = "README.md"
		names := []string{}
		for k := range files {
			names = append(names, k)
		}
		sort.Strings(names)
		out, err := os.Create("dist/spry-bridge-" + target + ".tar.gz")
		if err != nil {
			return err
		}
		gz := gzip.NewWriter(out)
		tw := tar.NewWriter(gz)
		for _, name := range names {
			b, err := os.ReadFile(files[name])
			if err != nil {
				return err
			}
			mode := int64(0644)
			if strings.HasPrefix(name, "bin/") {
				mode = 0755
			}
			if err = tw.WriteHeader(&tar.Header{Name: name, Mode: mode, Size: int64(len(b)), ModTime: time.Unix(0, 0)}); err != nil {
				return err
			}
			if _, err = tw.Write(b); err != nil {
				return err
			}
		}
		if err = tw.Close(); err != nil {
			return err
		}
		if err = gz.Close(); err != nil {
			return err
		}
		if err = out.Close(); err != nil {
			return err
		}
	}
	entries, err := os.ReadDir("dist")
	if err != nil {
		return err
	}
	var sums strings.Builder
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".tar.gz") {
			continue
		}
		f, err := os.Open(filepath.Join("dist", e.Name()))
		if err != nil {
			return err
		}
		h := sha256.New()
		_, err = io.Copy(h, f)
		_ = f.Close()
		if err != nil {
			return err
		}
		fmt.Fprintf(&sums, "%s  %s\n", hex.EncodeToString(h.Sum(nil)), e.Name())
	}
	return os.WriteFile("dist/SHA256SUMS", []byte(sums.String()), 0644)
}
