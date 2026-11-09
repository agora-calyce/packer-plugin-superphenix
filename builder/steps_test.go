package builder

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/agora-calyce/packer-plugin-superphenix/internal/client"
	"github.com/hashicorp/packer-plugin-sdk/multistep"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"
)

// Fake Superphenix: create -> eid i1; get -> Running + IP; call trace.
func fake(t *testing.T) (*client.Client, *[]string) {
	var mu sync.Mutex
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls = append(calls, r.Method+" "+strings.TrimPrefix(r.URL.Path, "/o/api/spx-ctrl/az/p/"))
		mu.Unlock()
		switch {
		case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/disk"):
			w.Write([]byte(`{"eid":"d1"}`))
		case r.Method == "POST":
			w.Write([]byte(`{"eid":"i1"}`))
		case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/i1"):
			w.Write([]byte(`{"eid":"i1","vmi":{"status":{"phase":"Running","interfaces":[{"ipAddress":"10.0.0.5"}]}}}`))
		}
	}))
	t.Cleanup(srv.Close)
	return client.New(srv.URL, "o", "az", "p", "tok"), &calls
}

func bag(api *client.Client) multistep.StateBag {
	s := new(multistep.BasicStateBag)
	s.Put("client", api)
	s.Put("ui", &packersdk.MockUi{})
	s.Put("config", &Config{CPU: 2, Memory: "4Gi", DiskSize: "20Gi", SourceURL: "http://img"})
	return s
}

func TestCreateThenCleanupOnFailure(t *testing.T) {
	api, calls := fake(t)
	state := bag(api)
	st := &stepCreate{}
	if st.Run(context.Background(), state) != multistep.ActionContinue {
		t.Fatal("create should continue")
	}
	if state.Get("ip") != "10.0.0.5" || state.Get("instance_id") != "i1" {
		t.Fatalf("bad state: %v %v", state.Get("ip"), state.Get("instance_id"))
	}
	st.Cleanup(state) // success: no deletion
	if strings.Contains(strings.Join(*calls, ","), "DELETE") {
		t.Fatal("must not delete on success")
	}
	state.Put("error", context.Canceled) // later failure
	st.Cleanup(state)
	got := strings.Join(*calls, ",")
	if !strings.HasSuffix(got, "DELETE instance/i1,DELETE disk/d1") {
		t.Fatalf("expected instance then disk deleted on failure, got %v", got)
	}
}

func TestCreateBodyShape(t *testing.T) {
	b, _ := json.Marshal(client.CreateVMInfo{Disks: []client.Disk{{EID: "d1"}}})
	if !strings.Contains(string(b), `"eid":"d1"`) {
		t.Fatal(string(b))
	}
}
