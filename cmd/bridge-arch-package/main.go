// bridge-arch-package creates deterministic, source-only local makepkg input.
// It does not build, install, sign, publish, or start a package.
package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const packageName = "spry-ai-workstation-bridge"

var stableVersion = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

var sourceRoots = []string{"cmd", "internal", "api", "deployment", "docs", "scripts", "packaging", ".github"}
var sourceFiles = []string{".go-version", "go.mod", "go.sum", "Makefile", "README.md"}

type config struct {
	root    string
	output  string
	version string
}

func main() {
	version := flag.String("version", "", "stable version in vMAJOR.MINOR.PATCH format")
	output := flag.String("output", "", "optional new local source output directory")
	flag.Parse()
	root, err := os.Getwd()
	if err != nil {
		fail(err)
	}
	destination := filepath.Join(root, "dist", "arch")
	if *output != "" {
		if _, err := os.Lstat(*output); !os.IsNotExist(err) {
			fail(fmt.Errorf("explicit output must not exist"))
		}
		destination = *output
	}
	if err := run(config{root: root, output: destination, version: *version}); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

func run(cfg config) error {
	pkgver, err := parseVersion(cfg.version)
	if err != nil {
		return err
	}
	if cfg.root == "" || cfg.output == "" {
		return errors.New("source root and output directory are required")
	}
	files, err := collectFiles(cfg.root)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(cfg.output, 0755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	archiveName := fmt.Sprintf("spry-bridge-%s-src.tar.gz", pkgver)
	archivePath := filepath.Join(cfg.output, archiveName)
	if err = writeArchiveAtomically(archivePath, packageName+"-"+pkgver, cfg.root, files); err != nil {
		return err
	}
	archiveSum, err := sha256File(archivePath)
	if err != nil {
		return err
	}
	template, err := os.ReadFile(filepath.Join(cfg.root, "packaging", "arch", "PKGBUILD.in"))
	if err != nil {
		return fmt.Errorf("read PKGBUILD template: %w", err)
	}
	pkgbuild, err := renderTemplate(string(template), pkgver, archiveName, archiveSum)
	if err != nil {
		return err
	}
	pkgbuildPath := filepath.Join(cfg.output, "PKGBUILD")
	if err = writeFileAtomically(pkgbuildPath, []byte(pkgbuild), 0644); err != nil {
		return err
	}
	pkgbuildSum, err := sha256File(pkgbuildPath)
	if err != nil {
		return err
	}
	sums := fmt.Sprintf("%s  %s\n%s  PKGBUILD\n", archiveSum, archiveName, pkgbuildSum)
	return writeFileAtomically(filepath.Join(cfg.output, "SHA256SUMS"), []byte(sums), 0644)
}

func parseVersion(version string) (string, error) {
	if !stableVersion.MatchString(version) {
		return "", errors.New("version must be a stable vMAJOR.MINOR.PATCH tag without leading zeroes or suffixes")
	}
	return strings.TrimPrefix(version, "v"), nil
}

func collectFiles(root string) ([]string, error) {
	files := make(map[string]struct{})
	for _, item := range sourceFiles {
		if err := addFile(root, item, files); err != nil {
			return nil, err
		}
	}
	for _, sourceRoot := range sourceRoots {
		path := filepath.Join(root, sourceRoot)
		info, err := os.Lstat(path)
		if err != nil {
			return nil, fmt.Errorf("inspect allowed source root %q: %w", sourceRoot, err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("allowed source root %q is not a directory", sourceRoot)
		}
		err = filepath.WalkDir(path, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			return addFile(root, rel, files)
		})
		if err != nil {
			return nil, fmt.Errorf("collect allowed source root %q: %w", sourceRoot, err)
		}
	}
	ordered := make([]string, 0, len(files))
	for file := range files {
		ordered = append(ordered, file)
	}
	sort.Strings(ordered)
	return ordered, nil
}

func addFile(root, relative string, files map[string]struct{}) error {
	if filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("source path escapes root: %q", relative)
	}
	path := filepath.Join(root, relative)
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("source symlink refused: %q", relative)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("source is not a regular file: %q", relative)
	}
	files[filepath.ToSlash(relative)] = struct{}{}
	return nil
}

func writeArchiveAtomically(destination, topLevel, root string, files []string) (err error) {
	temporary, err := os.CreateTemp(filepath.Dir(destination), ".arch-source-*")
	if err != nil {
		return fmt.Errorf("create source archive: %w", err)
	}
	temporaryName := temporary.Name()
	defer func() {
		if err != nil {
			_ = os.Remove(temporaryName)
		}
	}()
	gzipWriter := gzip.NewWriter(temporary)
	gzipWriter.ModTime = time.Unix(0, 0)
	tarWriter := tar.NewWriter(gzipWriter)
	for _, relative := range files {
		path := filepath.Join(root, filepath.FromSlash(relative))
		listedInfo, statErr := os.Lstat(path)
		if statErr != nil {
			err = statErr
			break
		}
		if listedInfo.Mode()&os.ModeSymlink != 0 || !listedInfo.Mode().IsRegular() {
			err = fmt.Errorf("source changed to an unsafe file while archiving: %q", relative)
			break
		}
		file, openErr := os.Open(path)
		if openErr != nil {
			err = openErr
			break
		}
		openedInfo, openedStatErr := file.Stat()
		finalInfo, finalStatErr := os.Lstat(path)
		if openedStatErr != nil || finalStatErr != nil || finalInfo.Mode()&os.ModeSymlink != 0 || !finalInfo.Mode().IsRegular() || !os.SameFile(listedInfo, openedInfo) || !os.SameFile(listedInfo, finalInfo) {
			_ = file.Close()
			err = fmt.Errorf("source changed while opening archive input: %q", relative)
			break
		}
		mode := int64(0644)
		if openedInfo.Mode().Perm()&0111 != 0 {
			mode = 0755
		}
		header := &tar.Header{Name: topLevel + "/" + relative, Mode: mode, Size: openedInfo.Size(), ModTime: time.Unix(0, 0), AccessTime: time.Unix(0, 0), ChangeTime: time.Unix(0, 0), Format: tar.FormatPAX}
		if headerErr := tarWriter.WriteHeader(header); headerErr != nil {
			_ = file.Close()
			err = headerErr
			break
		}
		_, copyErr := io.Copy(tarWriter, file)
		closeErr := file.Close()
		if copyErr != nil {
			err = copyErr
			break
		}
		if closeErr != nil {
			err = closeErr
			break
		}
	}
	if closeErr := tarWriter.Close(); err == nil && closeErr != nil {
		err = closeErr
	}
	if closeErr := gzipWriter.Close(); err == nil && closeErr != nil {
		err = closeErr
	}
	if closeErr := temporary.Close(); err == nil && closeErr != nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("write source archive: %w", err)
	}
	if err := os.Chmod(temporaryName, 0644); err != nil {
		return err
	}
	if err := os.Rename(temporaryName, destination); err != nil {
		return fmt.Errorf("publish source archive: %w", err)
	}
	return nil
}

func renderTemplate(template, pkgver, archiveName, archiveSum string) (string, error) {
	if strings.ContainsAny(pkgver+archiveName+archiveSum, "\r\n'") {
		return "", errors.New("unsafe package template value")
	}
	for _, placeholder := range []string{"@PKGVER@", "@SOURCE_ARCHIVE@", "@SOURCE_SHA256@"} {
		if strings.Count(template, placeholder) != 1 {
			return "", fmt.Errorf("PKGBUILD template must contain exactly one %s placeholder", placeholder)
		}
	}
	result := strings.NewReplacer(
		"@PKGVER@", pkgver,
		"@SOURCE_ARCHIVE@", archiveName,
		"@SOURCE_SHA256@", archiveSum,
	).Replace(template)
	return result, nil
}

func writeFileAtomically(destination string, contents []byte, mode fs.FileMode) (err error) {
	temporary, err := os.CreateTemp(filepath.Dir(destination), ".publish-*")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer func() {
		if err != nil {
			_ = os.Remove(temporaryName)
		}
	}()
	if err = temporary.Chmod(mode); err == nil {
		_, err = temporary.Write(contents)
	}
	if closeErr := temporary.Close(); err == nil && closeErr != nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(temporaryName, destination); err != nil {
		return err
	}
	return nil
}

func sha256File(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err = io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
