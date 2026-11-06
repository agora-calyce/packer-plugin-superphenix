package client

import (
	"context"
	"errors"
	"time"
)

// Types alignés sur vm.CreateVMInfo / view.Instance (docs.go du controller).
type CreateVMInfo struct {
	CloudInit *CloudInit `json:"cloudInit,omitempty"`
	Compute   Compute    `json:"compute"`
	Disks     []Disk     `json:"disks"`
	General   General    `json:"general"`
	Network   []Network  `json:"network,omitempty"`
	OrgID     string     `json:"orgID,omitempty"`
	ProjectID string     `json:"projectID,omitempty"`
	// ResourceEffectiveID : valeur choisie côté client ? Non confirmé, laissé vide par défaut.
	ResourceEffectiveID string `json:"resourceEffectiveID,omitempty"`
	ResourceLocalID     string `json:"resourceLocalID,omitempty"`
}
type CloudInit struct {
	Bus    string `json:"bus,omitempty"`
	Config string `json:"config"`
	Custom bool   `json:"custom"`
}
type Compute struct {
	CPU    int    `json:"cpu"`
	Memory string `json:"memory"`
}
type General struct {
	Labels      []string `json:"labels,omitempty"`
	RunStrategy string   `json:"runStrategy,omitempty"`
	VMType      string   `json:"vmType,omitempty"`
}
type Disk struct {
	Bus   string          `json:"bus,omitempty"`
	CDROM bool            `json:"cdrom,omitempty"`
	Disk  *CreateDiskInfo `json:"disk,omitempty"` // nouveau disque
	EID   string          `json:"eid,omitempty"`  // disque existant
	Order int             `json:"order"`
}
type CreateDiskInfo struct {
	General DiskGeneral `json:"general"`
}
type DiskGeneral struct {
	Source       DiskSource `json:"source"`
	Storage      string     `json:"storage"`
	StorageClass string     `json:"storageClass,omitempty"`
}
type DiskSource struct {
	Type string `json:"type"` // ex. "Http" (valeur exacte à confirmer)
	URL  string `json:"url,omitempty"`
}
type Network struct {
	Enabled   bool   `json:"enabled"`
	Model     string `json:"model,omitempty"`
	Order     int    `json:"order"`
	SubnetEID string `json:"subnetEId,omitempty"`
}

// Instance : sous-ensemble de view.Instance utile au plugin (status KubeVirt VMI).
type Instance struct {
	EID string `json:"eid"`
	VM  struct {
		Status struct {
			PrintableStatus string `json:"printableStatus"`
		} `json:"status"`
	} `json:"vm"`
	VMI struct {
		Status struct {
			Phase      string `json:"phase"`
			Interfaces []struct {
				IPAddress string `json:"ipAddress"`
			} `json:"interfaces"`
		} `json:"status"`
	} `json:"vmi"`
}

// IP renvoie la première IP connue de l'instance, "" sinon.
func (i *Instance) IP() string {
	for _, n := range i.VMI.Status.Interfaces {
		if n.IPAddress != "" {
			return n.IPAddress
		}
	}
	return ""
}

func (c *Client) GetInstance(ctx context.Context, id string) (*Instance, error) {
	var i Instance
	return &i, c.Call(ctx, "GET", "instance/"+id, nil, &i)
}

// WaitIP attend que l'instance soit Running avec une IP.
func (c *Client) WaitIP(ctx context.Context, id string, every time.Duration) (string, error) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		i, err := c.GetInstance(ctx, id)
		if err != nil {
			return "", err
		}
		if i.VMI.Status.Phase == "Running" && i.IP() != "" {
			return i.IP(), nil
		}
		select {
		case <-ctx.Done():
			return "", errors.Join(ctx.Err(), errors.New("waiting for instance IP"))
		case <-t.C:
		}
	}
}

// --- Disque, EIP, Security Group (flux 3b) ---

type EIDResponse struct {
	EID string `json:"eid"`
}

// CreateDisk : POST /disk. La réponse doit contenir l'eid (non confirmé par la spec).
func (c *Client) CreateDisk(ctx context.Context, d CreateDiskInfo) (string, error) {
	var out EIDResponse
	if err := c.Call(ctx, "POST", "disk", d, &out); err != nil {
		return "", err
	}
	if out.EID == "" {
		return "", errors.New("create disk: response has no eid")
	}
	return out.EID, nil
}

// DeleteDiskIfExists ignore les 404 : le disque a pu partir avec sa VM.
func (c *Client) DeleteDiskIfExists(ctx context.Context, id string) error {
	if err := c.DeleteDisk(ctx, id); err != nil && !IsNotFound(err) {
		return err
	}
	return nil
}

// SetEIPTarget associe l'EIP (pré-alloué) à une IP interne ; "" la libère.
// POST /eip = eip.UpdateEIPInfo (spec.internalIP).
func (c *Client) SetEIPTarget(ctx context.Context, eipEID, internalIP string) error {
	body := map[string]any{"resourceEffectiveID": eipEID, "spec": map[string]any{"internalIP": internalIP}}
	return c.Call(ctx, "POST", "eip", body, nil)
}

// EIPAddress lit l'IP publique (eip.status.ip, v1.IptablesEIPStatus).
func (c *Client) EIPAddress(ctx context.Context, eipEID string) (string, error) {
	var out struct {
		EIP struct {
			Status struct {
				IP string `json:"ip"`
			} `json:"status"`
		} `json:"eip"`
	}
	if err := c.Call(ctx, "GET", "eip/"+eipEID, nil, &out); err != nil {
		return "", err
	}
	if out.EIP.Status.IP == "" {
		return "", errors.New("eip " + eipEID + ": no public ip in status")
	}
	return out.EIP.Status.IP, nil
}

// AllowSSH crée une SG autorisant tcp/22 depuis cidr vers les instances portant label key=value.
func (c *Client) AllowSSH(ctx context.Context, subnetEID, key, value, cidr string) (string, error) {
	body := map[string]any{
		"description": "packer export helper",
		"subnetEIds":  []string{subnetEID},
		"target":      map[string]any{"matchLabels": []map[string]string{{"key": key, "value": value}}},
		"ingress": []map[string]any{{
			"from":  []map[string]any{{"IPBlock": map[string]any{"CIDR": cidr}}},
			"ports": []map[string]any{{"protocol": "TCP", "port": 22}},
		}},
	}
	var out EIDResponse
	if err := c.Call(ctx, "POST", "security-group", body, &out); err != nil {
		return "", err
	}
	if out.EID == "" {
		return "", errors.New("create security group: response has no eid")
	}
	return out.EID, nil
}

func (c *Client) DeleteSecurityGroup(ctx context.Context, id string) error {
	return c.Call(ctx, "DELETE", "security-group/"+id, nil, nil)
}

// WaitStopped attend que la VM soit "Stopped" (le endpoint stop rend la main avant l'arrêt effectif).
func (c *Client) WaitStopped(ctx context.Context, id string, every time.Duration) error {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		i, err := c.GetInstance(ctx, id)
		if err != nil {
			return err
		}
		if i.VM.Status.PrintableStatus == "Stopped" {
			return nil
		}
		select {
		case <-ctx.Done():
			return errors.Join(ctx.Err(), errors.New("waiting for instance to stop"))
		case <-t.C:
		}
	}
}
