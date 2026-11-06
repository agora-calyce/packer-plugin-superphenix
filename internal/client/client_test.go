package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/org/api/spx-ctrl/az1/proj/instance/abc/stop" || r.Header.Get("Authorization") != "Bearer tok" {
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
