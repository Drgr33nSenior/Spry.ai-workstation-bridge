package performance

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/domain"
)

func testBundle(t *testing.T) (string, string) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	b := []byte(`{"schema":1,"kind":"coding-eval","responses":"responses.json","corpus":"corpus.json"}`)
	if err = os.WriteFile(filepath.Join(root, "spec.json"), b, 0600); err != nil {
		t.Fatal(err)
	}
	m := Manifest{Schema: 1, Kind: "coding-eval", Target: "fixture", SourceRevision: strings.Repeat("a", 64), Files: map[string]string{"spec.json": Sum(b)}}
	b, _ = json.Marshal(m)
	if err = os.WriteFile(filepath.Join(root, "manifest.json"), b, 0600); err != nil {
		t.Fatal(err)
	}
	return root, Sum(b)
}
func TestPerformanceTransportPathsHashesPrivacyAndCancellation(t *testing.T) {
	ctx := context.Background()
	for _, change := range []string{"file", "directory", "symlink", "mode", "hash", "missing"} {
		t.Run(change, func(t *testing.T) {
			root, sha := testBundle(t)
			if _, err := Verify(ctx, root, sha); err != nil {
				t.Fatal(err)
			}
			var err error
			switch change {
			case "file":
				err = os.WriteFile(filepath.Join(root, "extra.json"), []byte(`{}`), 0600)
			case "directory":
				err = os.Mkdir(filepath.Join(root, "extra"), 0700)
			case "symlink":
				err = os.Symlink("spec.json", filepath.Join(root, "extra"))
			case "mode":
				err = os.Chmod(filepath.Join(root, "spec.json"), 0644)
			case "hash":
				err = os.WriteFile(filepath.Join(root, "spec.json"), []byte(`{}`), 0600)
			case "missing":
				err = os.Rename(filepath.Join(root, "spec.json"), filepath.Join(root, "preserved.json"))
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err = Verify(ctx, root, sha); err == nil {
				t.Fatal("unsafe tree accepted")
			}
		})
	}
	root, sha := testBundle(t)
	dst := filepath.Join(filepath.Dir(root), "retained-input")
	if err := Copy(ctx, root, dst, sha); err != nil {
		t.Fatal(err)
	}
	if err := Copy(ctx, root, dst, sha); err == nil {
		t.Fatal("overwrote evidence")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := Verify(cancelled, root, sha); err == nil {
		t.Fatal("cancelled verification succeeded")
	}
	for _, name := range []string{"../x", "/x", "a/../../x", "a//b", "a/./b", ".", "a\\b"} {
		if Relative(name) {
			t.Fatal("path accepted", name)
		}
	}
}
func TestPerformanceSummaryRejectsRawOrUnboundedArtifacts(t *testing.T) {
	s := domain.PerformanceSummary{Schema: 1, Kind: "cache", SHA256: strings.Repeat("a", 64), Status: "plan-only-unqualified", Reason: "retained"}
	if err := ValidateSummary(s); err != nil {
		t.Fatal(err)
	}
	a := domain.Artifact{Name: "plan.json", SHA256: strings.Repeat("b", 64), Size: 12, Content: "private sentinel"}
	s.Artifacts = []domain.Artifact{a}
	if ValidateSummary(s) == nil {
		t.Fatal("raw content in summary")
	}
	s.Artifacts[0].Content = ""
	if err := ValidateSummary(s); err != nil {
		t.Fatal(err)
	}
	s.Artifacts = append(s.Artifacts, s.Artifacts[0])
	if ValidateSummary(s) == nil {
		t.Fatal("duplicate artifact")
	}
}
