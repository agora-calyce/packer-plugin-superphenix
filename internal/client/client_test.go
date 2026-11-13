package client

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/websocket"
)

func TestCall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/org/api/spx-ctrl/az1/proj/instance/abc/stop" || r.Header.Get("Authorization") != "api-token tok" {
			http.Error(w, "bad request", 400)
		}
	}))
	defer srv.Close()
	c := New(srv.URL, "org", "az1", "proj", "tok")
	if err := c.StopInstance(context.Background(), "abc"); err != nil {
		t.Fatal(err)
	}
	if err := c.StopInstance(context.Background(), "zzz"); err == nil {
		t.Fatal("error expected on 400 response")
	}
}

func TestDeleteDiskIfExists(t *testing.T) {
	code := 404
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(code) }))
	defer srv.Close()
	c := New(srv.URL, "org", "az1", "proj", "tok")
	if err := c.DeleteDiskIfExists(context.Background(), "d"); err != nil {
		t.Fatalf("404 must be ignored: %v", err)
	}
	code = 400 // not retryable (5xx are, see TestDeleteDiskRetriesOn500)
	if err := c.DeleteDiskIfExists(context.Background(), "d"); err == nil {
		t.Fatal("error expected on 400")
	}
}

func TestInstanceIP(t *testing.T) {
	var i Instance
	if err := json.Unmarshal([]byte(`{"vmi":{"status":{"interfaces":[{"ipAddresses":["fe80::1","10.0.0.2"]}]}}}`), &i); err != nil {
		t.Fatal(err)
	}
	if got := i.IP(); got != "10.0.0.2" {
		t.Fatalf("IPv4 de ipAddresses attendue, obtenu %q", got)
	}
	if (&Instance{}).IP() != "" {
		t.Fatal("empty IP expected without interface")
	}
}

func TestGiB(t *testing.T) {
	for in, want := range map[string]string{"20Gi": "20", "20": "20", "5000Gi": "5000"} {
		if got, err := GiB(in); err != nil || got != want {
			t.Fatalf("GiB(%q) = %q, %v ; attendu %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "20G", "20Mi", "0", "5001Gi", "abc", "1.5Gi"} {
		if _, err := GiB(bad); err == nil {
			t.Fatalf("GiB(%q) should fail", bad)
		}
	}
}

func TestUserCloudInit(t *testing.T) {
	got := UserCloudInit("spx", "ssh-ed25519 AAAA\n", "qemu-utils")
	for _, want := range []string{"#cloud-config\n", "packages: [qemu-utils]", "name: spx", "NOPASSWD", "- ssh-ed25519 AAAA\n"} {
		if !strings.Contains(got, want) {
			t.Fatalf("%q absent de :\n%s", want, got)
		}
	}
}

func TestDeleteDiskRetriesWhileMounted(t *testing.T) {
	mountedRetry = time.Millisecond
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls++; calls < 3 {
			http.Error(w, `{"reason":"Cannot delete mounted disk"}`, 500)
		}
	}))
	defer srv.Close()
	if err := New(srv.URL, "o", "az", "p", "t").DeleteDiskIfExists(context.Background(), "d"); err != nil || calls != 3 {
		t.Fatalf("expected 3 calls without error, got %d calls, err=%v", calls, err)
	}
}

func TestWaitDiskReady(t *testing.T) {
	phases := []string{"Running", "", "Succeeded"} // "": transient 502, must be retried
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if p := phases[min(n, 2)]; p == "" {
			w.WriteHeader(http.StatusBadGateway)
		} else {
			w.Write([]byte(`{"disk":{"status":{"phase":"` + p + `"}}}`))
		}
		n++
	}))
	defer srv.Close()
	if err := New(srv.URL, "o", "az", "p", "t").WaitDiskReady(context.Background(), "d", time.Millisecond); err != nil || n != 3 {
		t.Fatalf("expected 3 calls without error, got %d, err=%v", n, err)
	}
}

func TestWaitFailsOnPermanentError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNotFound) }))
	defer srv.Close()
	if err := New(srv.URL, "o", "az", "p", "t").WaitStopped(context.Background(), "i", time.Millisecond); !IsNotFound(err) {
		t.Fatalf("a 404 must stop the wait, got %v", err)
	}
}

func TestMergeUserData(t *testing.T) {
	base := UserCloudInit("spx", "ssh-ed25519 AAAA")
	got, err := MergeUserData(base, "#cloud-config\nhostname: web\nusers:\n  - name: bob\npackages: [nginx]\n")
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range []string{"#cloud-config", "hostname: web", "name: bob", "name: spx", "ssh-ed25519 AAAA", "nginx"} {
		if !strings.Contains(got, w) {
			t.Errorf("missing %q in:\n%s", w, got)
		}
	}
	if same, _ := MergeUserData(base, ""); same != base {
		t.Error("empty user_data must keep base")
	}
	if _, err := MergeUserData(base, "#!/bin/sh\necho hi"); err == nil {
		t.Error("non cloud-config must be rejected")
	}
	if _, err := MergeUserData(base, "#cloud-config\nusers: bob\n"); err == nil {
		t.Error("non-list users must be rejected")
	}
}

func TestVNCKeyEvent(t *testing.T) {
	got := make(chan []byte, 1)
	srv := httptest.NewServer(websocket.Handler(func(ws *websocket.Conn) {
		ws.PayloadType = websocket.BinaryFrame
		ws.Write([]byte("RFB 003.008\n"))
		io.ReadFull(ws, make([]byte, 12))
		ws.Write([]byte{1, 1})
		io.ReadFull(ws, make([]byte, 1))
		ws.Write([]byte{0, 0, 0, 0})
		io.ReadFull(ws, make([]byte, 1))
		ws.Write(append(make([]byte, 20), 0, 0, 0, 2, 'v', 'm'))
		m := make([]byte, 8)
		io.ReadFull(ws, m)
		got <- m
	}))
	defer srv.Close()
	v, err := New(srv.URL, "o", "az", "p", "tok").DialVNC(context.Background(), "i1")
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	if err := v.KeyEvent(0xFF0D, true); err != nil {
		t.Fatal(err)
	}
	if m := <-got; !bytes.Equal(m, []byte{4, 1, 0, 0, 0, 0, 0xFF, 0x0D}) {
		t.Fatalf("bad key event %v", m)
	}
}

func TestDeleteDiskRetriesOn500(t *testing.T) {
	mountedRetry = time.Millisecond
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if n++; n < 3 {
			http.Error(w, "boom", 500)
		}
	}))
	defer srv.Close()
	if err := New(srv.URL, "o", "az", "p", "tok").DeleteDiskIfExists(context.Background(), "d1"); err != nil || n != 3 {
		t.Fatalf("err=%v calls=%d", err, n)
	}
}

func TestCreateInstanceMemoryFormat(t *testing.T) {
	var bodies []string
	legacyAPI := true // API < 0.10: rejects a string memory
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(b))
		if legacyAPI && strings.Contains(string(b), `"memory":"`) {
			http.Error(w, "cannot unmarshal string into field compute.memory of type int", 400)
			return
		}
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	c := New(srv.URL, "org", "az1", "proj", "tok")
	spec := CreateVMInfo{Compute: Compute{CPU: 2, Memory: 4}}
	if err := c.CreateInstance(context.Background(), spec, nil); err != nil {
		t.Fatal(err)
	}
	if len(bodies) != 2 || !strings.Contains(bodies[0], `"memory":"4Gi"`) || !strings.Contains(bodies[1], `"memory":4`) {
		t.Fatalf("expected quantity then legacy retry, got %v", bodies)
	}
	if err := c.CreateInstance(context.Background(), spec, nil); err != nil || len(bodies) != 3 || !strings.Contains(bodies[2], `"memory":4`) {
		t.Fatalf("legacy form must be remembered: %v %v", err, bodies)
	}

	bodies, legacyAPI = nil, false
	c = New(srv.URL, "org", "az1", "proj", "tok")
	if err := c.CreateInstance(context.Background(), spec, nil); err != nil || len(bodies) != 1 {
		t.Fatalf("new API: single quantity request expected: %v %v", err, bodies)
	}
}

func TestMergeUserDataKeepsYAML11Booleans(t *testing.T) {
	got, err := MergeUserData(UserCloudInit("spx", "ssh-ed25519 AAAA"), "#cloud-config\nusers:\n  - name: bob\n    lock_passwd: no\n")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "lock_passwd: no\n") || strings.Count(got, "#cloud-config") != 1 {
		t.Errorf("scalars must be kept as written, one header expected:\n%s", got)
	}
}

func TestDeleteInstanceRetriesOn5xx(t *testing.T) {
	deleteInstanceRetry = time.Millisecond
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls++; calls < 3 {
			http.Error(w, "bad gateway", 502)
		}
	}))
	defer srv.Close()
	if err := New(srv.URL, "o", "az", "p", "t").DeleteInstance(context.Background(), "i"); err != nil || calls != 3 {
		t.Fatalf("expected 3 calls without error, got %d calls, err=%v", calls, err)
	}
}

// Prepare validates user_data against a base that holds only the #cloud-config line: it used to panic.
func TestMergeUserDataEmptyBase(t *testing.T) {
	got, err := MergeUserData("#cloud-config\n", "#cloud-config\nhostname: web\nusers:\n  - name: bob\npackages: [nginx]\n")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"#cloud-config\n", "hostname: web", "name: bob", "nginx"} {
		if !strings.Contains(got, want) {
			t.Errorf("merged config lacks %q:\n%s", want, got)
		}
	}
}
