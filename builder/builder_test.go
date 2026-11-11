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
