package postprocessor

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/agora-calyce/packer-plugin-superphenix/internal/client"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"
	"golang.org/x/crypto/ssh"
	"gopkg.in/yaml.v3"
)

func run(t *testing.T, transferErr error) []string {
	var mu sync.Mutex
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls = append(calls, r.Method+" "+strings.TrimPrefix(r.URL.Path, "/o/api/spx-ctrl/az/p/"))
		mu.Unlock()
		switch {
		case r.Method == "DELETE" && strings.HasSuffix(r.URL.Path, "/instance/i1"):
			// was the Packer VM already deleted by a previous call? no: first call OK
		case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/disk/b1"):
			w.Write([]byte(`{"disk":{"status":{"phase":"Succeeded"}}}`))
		case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/disk"):
			w.Write([]byte(`{"eid":"b1"}`)) // helper boot disk
		case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/instance"):
			w.Write([]byte(`{"eid":"h1"}`))
		case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/instance/h1"):
			w.Write([]byte(`{"eid":"h1","vmi":{"status":{"phase":"Running","interfaces":[{"ipAddress":"10.0.0.9"}]}}}`))
		case r.Method == "DELETE" && strings.HasSuffix(r.URL.Path, "/disk/d1"):
			http.Error(w, "gone", 404) // gone with the helper: must be ignored
		}
	}))
	defer srv.Close()

	api := client.New(srv.URL, "o", "az", "p", "tok")
	p := &PostProcessor{transfer: func(_ context.Context, _ packersdk.Ui, host, _ string, _ ssh.Signer, _ ssh.PublicKey, _, _ string) error {
		if host != "10.0.0.9" {
			t.Errorf("host = %s, want helper private IP", host)
		}
		return transferErr
	}}
	if err := p.Configure(map[string]any{"output_path": "/tmp/x.qcow2", "api_url": srv.URL, "org_id": "o", "az": "az", "project_id": "p", "token": "tok", "helper_image_url": "http://img",
		"subnet_eid": "s1", "storage_class": "block-standard"}); err != nil {
		t.Fatal(err)
	}
	err := p.export(context.Background(), &packersdk.MockUi{}, api, "i1", "d1")
	if (err != nil) != (transferErr != nil) {
		t.Fatalf("err = %v, transferErr = %v", err, transferErr)
	}
	return calls
}

func TestExportOrderAndCleanup(t *testing.T) {
	for _, terr := range []error{nil, errors.New("boom")} { // cleanup must be identical in both cases
		got := strings.Join(run(t, terr), ",")
		for _, want := range []string{
			"GET disk/d1/unmount,DELETE instance/i1",                 // detach then delete the Packer VM
			"DELETE instance/h1", "DELETE disk/d1", "DELETE disk/b1", // b1 = helper boot disk, deleted explicitly
		} {
			if !strings.Contains(got, want) {
				t.Errorf("transferErr=%v: missing %q in %s", terr, want, got)
			}
		}
	}
}

func TestEnsureOutputDir(t *testing.T) {
	out := filepath.Join(t.TempDir(), "a", "b", "image.qcow2")
	if err := ensureOutputDir(out); err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(filepath.Dir(out)); err != nil || !st.IsDir() {
		t.Fatalf("parent directory not created: %v", err)
	}
	if err := ensureOutputDir(out); err != nil { // already present: no effect
		t.Fatal(err)
	}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("no space left on device") }

func TestLocalWriterKeepsFirstError(t *testing.T) {
	lw := &localWriter{w: failWriter{}}
	if _, err := lw.Write([]byte("x")); err == nil || lw.err == nil {
		t.Fatalf("write error not kept: %v / %v", err, lw.err)
	}
}

func TestFreeBytes(t *testing.T) {
	if free, err := freeBytes(t.TempDir()); err != nil || free == 0 {
		t.Skipf("free space unavailable here: %d, %v", free, err)
	}
}

// The cloud-init block must be valid YAML carrying the private half of the key the download pins.
func TestHelperHostKey(t *testing.T) {
	ci, pub, err := helperHostKey()
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Keys struct {
			Private string `yaml:"ed25519_private"`
			Public  string `yaml:"ed25519_public"`
		} `yaml:"ssh_keys"`
	}
	if err := yaml.Unmarshal([]byte(client.UserCloudInit("spx", "ssh-ed25519 AAAA", "qemu-utils")+ci), &doc); err != nil {
		t.Fatal(err, ci)
	}
	signer, err := ssh.ParsePrivateKey([]byte(doc.Keys.Private))
	if err != nil {
		t.Fatal(err)
	}
	want := string(ssh.MarshalAuthorizedKey(pub))
	if got := string(ssh.MarshalAuthorizedKey(signer.PublicKey())); got != want || doc.Keys.Public+"\n" != want {
		t.Fatalf("host key mismatch: private gives %q, public is %q, pinned %q", got, doc.Keys.Public, want)
	}
}

func TestCheckOutputFree(t *testing.T) {
	out := filepath.Join(t.TempDir(), "image.qcow2")
	if err := checkOutputFree(out, false); err != nil {
		t.Fatalf("missing file must be accepted: %v", err)
	}
	if err := os.WriteFile(out, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := checkOutputFree(out, false); err == nil {
		t.Fatal("existing output must be refused without -force")
	}
	if err := checkOutputFree(out, true); err != nil {
		t.Fatalf("-force must allow overwriting: %v", err)
	}
}
