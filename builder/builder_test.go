package builder

import "testing"

func TestBootCommandWithSourceURL(t *testing.T) {
	cfg := func(extra map[string]interface{}) map[string]interface{} {
		m := map[string]interface{}{"api_url": "https://a", "org_id": "o", "az": "az", "project_id": "p", "token": "t",
			"subnet_eid": "s", "storage_class": "sc", "boot_command": []string{"<enter>"}}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}
	for name, extra := range map[string]map[string]interface{}{
		"source_url": {"source_url": "https://img"},
		"iso_url":    {"iso_url": "https://iso"},
	} {
		if _, _, err := (&Builder{}).Prepare(cfg(extra)); err != nil {
			t.Errorf("%s with boot_command: %v", name, err)
		}
	}
	if _, _, err := (&Builder{}).Prepare(cfg(nil)); err == nil {
		t.Error("no source: want an error")
	}
}

// Prepare validates user_data: it must accept a valid one (it used to panic) and reject a malformed one.
func TestPrepareUserData(t *testing.T) {
	cfg := func(userData string) map[string]interface{} {
		return map[string]interface{}{"api_url": "https://a", "org_id": "o", "az": "az", "project_id": "p", "token": "t",
			"subnet_eid": "s", "storage_class": "sc", "source_url": "https://img", "user_data": userData}
	}
	if _, _, err := (&Builder{}).Prepare(cfg("#cloud-config\nhostname: web\nusers:\n  - name: bob\npackages: [nginx]\n")); err != nil {
		t.Errorf("valid user_data: %v", err)
	}
	if _, _, err := (&Builder{}).Prepare(cfg("#cloud-config\nusers: bob\n")); err == nil {
		t.Error("users that is not a list: want an error")
	}
	if _, _, err := (&Builder{}).Prepare(cfg("#!/bin/sh\necho hi")); err == nil {
		t.Error("not a #cloud-config: want an error")
	}
}
