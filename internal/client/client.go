// Package client : client HTTP minimal pour l'API Superphenix (stdlib uniquement).
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client cible /{orgId}/api/spx-ctrl/{az}/{projectId}/{resource} (proxy superphenix-api).
type Client struct {
	BaseURL, OrgID, AZ, ProjectID, Token string
	HTTP                                 *http.Client
}

func New(baseURL, orgID, az, projectID, token string) *Client {
	return &Client{strings.TrimRight(baseURL, "/"), orgID, az, projectID, token,
		&http.Client{Timeout: 60 * time.Second}}
}

// Call envoie une requête JSON ; in/out peuvent être nil. Toute réponse non 2xx est une erreur.
func (c *Client) Call(ctx context.Context, method, resource string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	url := fmt.Sprintf("%s/%s/api/spx-ctrl/%s/%s/%s", c.BaseURL, c.OrgID, c.AZ, c.ProjectID, resource)
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	if res.StatusCode/100 != 2 {
		return &StatusError{res.StatusCode, fmt.Sprintf("%s %s: %s: %s", method, resource, res.Status, strings.TrimSpace(string(data)))}
	}
	if out != nil && len(data) > 0 {
		return json.Unmarshal(data, out)
	}
	return nil
}

// StatusError : réponse HTTP non 2xx.
type StatusError struct {
	Code int
	Msg  string
}

func (e *StatusError) Error() string { return e.Msg }

// IsNotFound : vrai pour une 404 (ressource déjà supprimée).
func IsNotFound(err error) bool {
	var se *StatusError
	return errors.As(err, &se) && se.Code == http.StatusNotFound
}

// Wrappers des opérations utilisées par le flux 3b (voir DESIGN.md).
// Les corps de requête/réponse suivent vm.CreateVMInfo ; à typer une fois l'env de test dispo.

func (c *Client) CreateInstance(ctx context.Context, spec, out any) error {
	return c.Call(ctx, "POST", "instance", spec, out)
}
func (c *Client) StopInstance(ctx context.Context, id string) error {
	return c.Call(ctx, "GET", "instance/"+id+"/stop", nil, nil)
}
func (c *Client) DeleteInstance(ctx context.Context, id string) error {
	return c.Call(ctx, "DELETE", "instance/"+id, nil, nil)
}
func (c *Client) UnmountDisk(ctx context.Context, id string) error {
	return c.Call(ctx, "GET", "disk/"+id+"/unmount", nil, nil)
}
func (c *Client) DeleteDisk(ctx context.Context, id string) error {
	return c.Call(ctx, "DELETE", "disk/"+id, nil, nil)
}
