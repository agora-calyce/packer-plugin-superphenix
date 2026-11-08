package builder

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/agora-calyce/packer-plugin-superphenix/internal/client"
	"github.com/hashicorp/packer-plugin-sdk/multistep"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"
)

// Faux Superphenix : create -> eid i1 ; get -> Running + IP ; trace des appels.
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
	if st.Run(context.Background(), state) != multistep.ActionContinue ||
		(&stepWaitIP{}).Run(context.Background(), state) != multistep.ActionContinue {
		t.Fatal("create should continue")
	}
	if state.Get("ip") != "10.0.0.5" || state.Get("instance_id") != "i1" {
		t.Fatalf("bad state: %v %v", state.Get("ip"), state.Get("instance_id"))
	}
	state.Put("build_ok", true)
	st.Cleanup(state) // succès : pas de suppression
	if strings.Contains(strings.Join(*calls, ","), "DELETE") {
		t.Fatal("must not delete on success")
	}
	state = bag(api) // build non abouti (pas de build_ok), même sans error ni StateCancelled
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

func TestISOCreateBody(t *testing.T) {
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/disk"):
			w.Write([]byte(`{"eid":"d1"}`))
		case r.Method == "POST":
			body, _ = io.ReadAll(r.Body)
			w.Write([]byte(`{"eid":"i1"}`))
		default: // GET disk : import terminé
			w.Write([]byte(`{"disk":{"status":{"phase":"Succeeded"}}}`))
		}
	}))
	defer srv.Close()
	state := bag(client.New(srv.URL, "o", "az", "p", "tok"))
	state.Put("config", &Config{CPU: 2, Memory: "4Gi", DiskSize: "20Gi", ISOURL: "http://iso", ISODiskSize: "8Gi",
		UserData: "#cloud-config\nautoinstall:\n  version: 1\n"})
	if (&stepCreate{}).Run(context.Background(), state) != multistep.ActionContinue {
		t.Fatal("create should continue")
	}
	var got struct {
		CloudInit struct{ Config string }
		Disks     []struct {
			CDROM bool
			Order int
		}
		Advanced struct {
			Firmware struct {
				Bootloader struct{ EFI struct{ Enabled bool } }
			}
		}
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err, string(body))
	}
	if got.CloudInit.Config != "#cloud-config\nautoinstall:\n  version: 1\n" {
		t.Errorf("user_data must be passed as is, got %q", got.CloudInit.Config)
	}
	if len(got.Disks) != 2 || got.Disks[0].CDROM || !got.Disks[1].CDROM {
		t.Errorf("want empty disk first then ISO cdrom, got %+v", got.Disks)
	}
	if !got.Advanced.Firmware.Bootloader.EFI.Enabled {
		t.Error("EFI must be enabled by default in ISO mode")
	}
}
