package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
		t.Fatal("erreur attendue sur réponse 400")
	}
}

func TestDeleteDiskIfExists(t *testing.T) {
	code := 404
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(code) }))
	defer srv.Close()
	c := New(srv.URL, "org", "az1", "proj", "tok")
	if err := c.DeleteDiskIfExists(context.Background(), "d"); err != nil {
		t.Fatalf("404 doit être ignoré : %v", err)
	}
	code = 500
	if err := c.DeleteDiskIfExists(context.Background(), "d"); err == nil {
		t.Fatal("erreur attendue sur 500")
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
		t.Fatal("IP vide attendue sans interface")
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
			t.Fatalf("GiB(%q) devrait échouer", bad)
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
