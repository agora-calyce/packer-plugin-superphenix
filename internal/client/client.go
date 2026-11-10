// Package client: minimal HTTP client for the Superphenix API (stdlib only).
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

// Client targets /{orgId}/api/spx-ctrl/{az}/{projectId}/{resource} (superphenix-api proxy).
type Client struct {
	BaseURL, OrgID, AZ, ProjectID, Token string
	HTTP                                 *http.Client
	legacyMemory                         bool // set once the API refused the "4Gi" quantity form
}

func New(baseURL, orgID, az, projectID, token string) *Client {
	return &Client{strings.TrimRight(baseURL, "/"), orgID, az, projectID, token,
		&http.Client{Timeout: 60 * time.Second}, false}
}

// Call sends a JSON request; in/out may be nil. Any non-2xx response is an error.
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
	req.Header.Set("Authorization", "api-token "+c.Token) // console API token scheme
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

// StatusError: non-2xx HTTP response.
type StatusError struct {
	Code int
	Msg  string
}

func (e *StatusError) Error() string { return e.Msg }

// IsNotFound: true for a 404 (resource already deleted).
func IsNotFound(err error) bool {
	var se *StatusError
	return errors.As(err, &se) && se.Code == http.StatusNotFound
}

// Wrappers for the operations used by the build and export flows (see DESIGN.md).

// CreateInstance sends compute.memory as a quantity ("4Gi", API >= 0.10). If the API refuses it with a 400 that
// mentions memory (older API: integer GiB), it retries once in the legacy form and remembers it.
func (c *Client) CreateInstance(ctx context.Context, spec CreateVMInfo, out any) error {
	spec.Compute.Legacy = c.legacyMemory
	err := c.Call(ctx, "POST", "instance", spec, out)
	var se *StatusError
	if spec.Compute.Legacy || !errors.As(err, &se) || se.Code != http.StatusBadRequest || !strings.Contains(se.Msg, "memory") {
		return err
	}
	spec.Compute.Legacy = true
	if c.Call(ctx, "POST", "instance", spec, out) != nil {
		return err // not a format problem: report the original error
	}
	c.legacyMemory = true
	return nil
}
func (c *Client) StartInstance(ctx context.Context, id string) error {
	return c.Call(ctx, "GET", "instance/"+id+"/start", nil, nil)
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
