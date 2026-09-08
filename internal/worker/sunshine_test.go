package worker

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

func fixtureOCI(t *testing.T, corrupt, escape bool) ([]byte, string, string) {
	t.Helper()
	config := []byte(`{"architecture":"amd64","os":"linux"}`)
	cs := sha256.Sum256(config)
	configID := hex.EncodeToString(cs[:])
	manifest := []byte(fmt.Sprintf(`{"schemaVersion":2,"config":{"digest":"sha256:%s"},"layers":[]}`, configID))
	ms := sha256.Sum256(manifest)
	digest := hex.EncodeToString(ms[:])
	var b bytes.Buffer
	w := tar.NewWriter(&b)
	files := map[string][]byte{"oci-layout": []byte(`{"imageLayoutVersion":"1.0.0"}`), "index.json": []byte(fmt.Sprintf(`{"manifests":[{"digest":"sha256:%s"}]}`, digest)), "blobs/sha256/" + digest: manifest, "blobs/sha256/" + configID: config}
	if corrupt {
		files["blobs/sha256/"+configID] = []byte("tampered")
	}
	if escape {
		files["../escape"] = []byte("x")
	}
	for name, body := range files {
		if e := w.WriteHeader(&tar.Header{Name: name, Size: int64(len(body)), Mode: 0644, Typeflag: tar.TypeReg}); e != nil {
			t.Fatal(e)
		}
		w.Write(body)
	}
	if e := w.Close(); e != nil {
		t.Fatal(e)
	}
	return b.Bytes(), digest, "sha256:" + configID
}
func TestOfflineOCIDigestAndTraversal(t *testing.T) {
	b, digest, config := fixtureOCI(t, false, false)
	if e := verifyOCIArchive(bytes.NewReader(b), digest); e != nil {
		t.Fatal(e)
	}
	if c, e := ociConfigID(bytes.NewReader(b), digest); e != nil || c != config {
		t.Fatal(c, e)
	}
	if e := verifyOCIBase(bytes.NewReader(b)); e == nil {
		t.Fatal("fixture incorrectly accepted as reviewed Steam base")
	}
	for _, fault := range []struct{ corrupt, escape bool }{{true, false}, {false, true}} {
		b, digest, _ := fixtureOCI(t, fault.corrupt, fault.escape)
		if e := verifyOCIArchive(bytes.NewReader(b), digest); e == nil {
			t.Fatal("accepted corrupt/escaping archive")
		}
	}
}
func TestSunshineOfflineRecipePreservesGate(t *testing.T) {
	s := string(sunshineContainerfile)
	for _, required := range []string{"apt-get --no-download", "26.1.2-1~bpo13+1", "c87f226920ad83055a898be1f0c7540307593e92d8b0baf1f076909758db8ca0", "pending-display-input-and-security"} {
		if !strings.Contains(s, required) {
			t.Fatal("missing reviewed recipe contract", required)
		}
	}
	for _, forbidden := range []string{"apt-get update", "allow-unauthenticated", "curl ", "wget "} {
		if strings.Contains(s, forbidden) {
			t.Fatal("online or bypass option", forbidden)
		}
	}
}
