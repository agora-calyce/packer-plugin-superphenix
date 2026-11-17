package builder

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/agora-calyce/packer-plugin-superphenix/internal/client"
	"github.com/hashicorp/packer-plugin-sdk/multistep"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"
)

// statusAPI is a fake Superphenix whose instance i1 always reports the given VM status; stops counts stop calls.
func statusAPI(t *testing.T, status string) (*client.Client, *atomic.Int32) {
	return statusAPIStop(t, status, 200)
}

// statusAPIStop is statusAPI with the HTTP code the stop call answers.
func statusAPIStop(t *testing.T, status string, stopCode int) (*client.Client, *atomic.Int32) {
	var stops atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/stop") {
			stops.Add(1)
			w.WriteHeader(stopCode)
			return
		}
		w.Write([]byte(`{"eid":"i1","vm":{"status":{"printableStatus":"` + status + `"}}}`))
	}))
	t.Cleanup(srv.Close)
	return client.New(srv.URL, "o", "az", "p", "tok"), &stops
}

func sysprepBag(api *client.Client, c *Config, comm *packersdk.MockCommunicator) multistep.StateBag {
	s := bag(api)
	s.Put("config", c)
	s.Put("instance_id", "i1")
	s.Put("communicator", comm)
	return s
}

func decodePS(t *testing.T, cmd string) string {
	b, err := base64.StdEncoding.DecodeString(cmd[strings.LastIndex(cmd, " ")+1:])
	if err != nil {
		t.Fatal(err)
	}
	u := make([]uint16, len(b)/2)
	for i := range u {
		u[i] = uint16(b[2*i]) | uint16(b[2*i+1])<<8
	}
	return string(utf16.Decode(u))
}

func TestSysprepInactiveWithoutOption(t *testing.T) {
	api, _ := statusAPI(t, "Running")
	comm := &packersdk.MockCommunicator{}
	if (stepSysprepWindows{}).Run(context.Background(), sysprepBag(api, &Config{}, comm)) != multistep.ActionContinue || comm.StartCalled {
		t.Fatal("sysprep off: the step must do nothing")
	}
}

func TestSysprepLaunchesThenWaitsForStop(t *testing.T) {
	api, _ := statusAPI(t, "Stopped")
	comm := &packersdk.MockCommunicator{}
	c := &Config{Sysprep: true, SysprepUnattend: `C:\u.xml`, SysprepTimeout: time.Minute}
	if (stepSysprepWindows{}).Run(context.Background(), sysprepBag(api, c, comm)) != multistep.ActionContinue {
		t.Fatal("want continue")
	}
	script := decodePS(t, comm.StartCmd.Command)
	for _, want := range []string{"/generalize /oobe /shutdown /quiet", `/unattend:"C:\u.xml"`, "Start-ScheduledTask"} {
		if !strings.Contains(script, want) {
			t.Errorf("launch script lacks %q:\n%s", want, script)
		}
	}
	if strings.Contains(script, "-Wait") {
		t.Error("the launch must not wait for Sysprep")
	}
}

func TestSysprepTimeout(t *testing.T) {
	api, _ := statusAPI(t, "Running")
	state := sysprepBag(api, &Config{Sysprep: true, SysprepTimeout: 50 * time.Millisecond}, &packersdk.MockCommunicator{})
	if (stepSysprepWindows{}).Run(context.Background(), state) != multistep.ActionHalt {
		t.Fatal("VM still running: want halt")
	}
	err, _ := state.Get("error").(error)
	if err == nil || !strings.Contains(err.Error(), "setupact.log") {
		t.Fatalf("want an error pointing to setupact.log, got %v", err)
	}
}

// An already stopped VM (Sysprep /shutdown) must still get the stop call: it is what sets runStrategy to Halted,
// otherwise Superphenix restarts the VM.
func TestStopStillCalledWhenAlreadyStopped(t *testing.T) {
	api, stops := statusAPI(t, "Stopped")
	if (&stepStop{}).Run(context.Background(), sysprepBag(api, &Config{}, nil)) != multistep.ActionContinue || stops.Load() != 1 {
		t.Fatalf("already stopped: want continue and one stop call, got %d", stops.Load())
	}
}

func TestStopToleratesErrorOnlyWhenStopped(t *testing.T) {
	api, _ := statusAPIStop(t, "Stopped", 500)
	if (&stepStop{}).Run(context.Background(), sysprepBag(api, &Config{}, nil)) != multistep.ActionContinue {
		t.Fatal("stop error on a stopped VM: want continue")
	}
	api, _ = statusAPIStop(t, "Running", 500)
	if (&stepStop{}).Run(context.Background(), sysprepBag(api, &Config{}, nil)) != multistep.ActionHalt {
		t.Fatal("stop error on a running VM: want halt")
	}
}

func TestFinishStepsOrder(t *testing.T) {
	var got []string
	for _, s := range finishSteps() {
		got = append(got, strings.TrimPrefix(strings.TrimPrefix(typeName(s), "*"), "builder."))
	}
	if strings.Join(got, ",") != "stepFinalizeWindows,stepSysprepWindows,stepStop,stepDropISO" {
		t.Fatalf("bad order: %v", got)
	}
}

func TestPrepareSysprep(t *testing.T) {
	cfg := func(extra map[string]interface{}) map[string]interface{} {
		m := map[string]interface{}{"api_url": "https://a", "org_id": "o", "az": "az", "project_id": "p", "token": "t",
			"subnet_eid": "s", "storage_class": "sc", "source_url": "https://img"}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}
	win := map[string]interface{}{"communicator": "winrm", "winrm_username": "packer", "winrm_password": "x"}
	for name, extra := range map[string]map[string]interface{}{
		"sysprep on ssh":             {"sysprep": true},
		"unattend without sysprep":   {"communicator": "winrm", "winrm_username": "packer", "winrm_password": "x", "sysprep_unattend": `C:\u.xml`},
		"unattend with double quote": {"communicator": "winrm", "winrm_username": "packer", "winrm_password": "x", "sysprep": true, "sysprep_unattend": `C:\"u.xml`},
	} {
		if _, _, err := (&Builder{}).Prepare(cfg(extra)); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
	ok := cfg(win)
	ok["sysprep"] = true
	if _, _, err := (&Builder{}).Prepare(ok); err != nil {
		t.Errorf("sysprep on winrm: %v", err)
	}
	if _, _, err := (&Builder{}).Prepare(cfg(nil)); err != nil {
		t.Errorf("linux without sysprep: %v", err)
	}
}

func typeName(v interface{}) string { return fmt.Sprintf("%T", v) }
