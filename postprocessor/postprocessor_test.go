package postprocessor

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/agora-calyce/packer-plugin-superphenix/internal/client"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"
	"golang.org/x/crypto/ssh"
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
			// la VM Packer a déjà été supprimée par un appel précédent ? non : premier appel OK
		case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/disk/b1"):
			w.Write([]byte(`{"disk":{"status":{"phase":"Succeeded"}}}`))
		case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/disk"):
			w.Write([]byte(`{"eid":"b1"}`)) // disque de boot du helper
		case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/instance"):
			w.Write([]byte(`{"eid":"h1"}`))
		case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/instance/h1"):
			w.Write([]byte(`{"eid":"h1","vmi":{"status":{"phase":"Running","interfaces":[{"ipAddress":"10.0.0.9"}]}}}`))
		case r.Method == "DELETE" && strings.HasSuffix(r.URL.Path, "/disk/d1"):
			http.Error(w, "gone", 404) // parti avec le helper : doit être ignoré
		}
	}))
	defer srv.Close()

	api := client.New(srv.URL, "o", "az", "p", "tok")
	p := &PostProcessor{transfer: func(_ context.Context, _ packersdk.Ui, host, _ string, _ ssh.Signer, _, _ string) error {
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
	for _, terr := range []error{nil, errors.New("boom")} { // le nettoyage doit être identique dans les deux cas
		got := strings.Join(run(t, terr), ",")
		for _, want := range []string{
			"GET disk/d1/unmount,DELETE instance/i1",                 // détache puis supprime la VM Packer
			"DELETE instance/h1", "DELETE disk/d1", "DELETE disk/b1", // b1 = boot du helper, supprimé explicitement
		} {
			if !strings.Contains(got, want) {
				t.Errorf("transferErr=%v: missing %q in %s", terr, want, got)
			}
		}
	}
}
