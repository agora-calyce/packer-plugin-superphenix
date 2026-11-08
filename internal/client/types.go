package client

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Types alignés sur vm.CreateVMInfo / view.Instance (docs.go du controller).
type CreateVMInfo struct {
	Advanced  *Advanced  `json:"advanced,omitempty"`
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
type Advanced struct {
	Devices  *Devices  `json:"devices,omitempty"`
	Firmware *Firmware `json:"firmware,omitempty"`
}
type Devices struct {
	TPM TPM `json:"tpm"`
}
type TPM struct {
	Enabled bool `json:"enabled"`
}
type Firmware struct {
	Bootloader Bootloader `json:"bootloader"`
}
type Bootloader struct {
	EFI EFI `json:"efi"`
}
type EFI struct {
	Enabled    bool `json:"enabled"`
	SecureBoot bool `json:"secureBoot"`
}
type CloudInit struct {
	Bus    string `json:"bus,omitempty"`
	Config string `json:"config"`
	Custom bool   `json:"custom"`
}
type Compute struct {
	CPU    int `json:"cpu"`
	Memory int `json:"memory"` // Gio
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
	Labels       []string   `json:"labels,omitempty"`
	Source       DiskSource `json:"source"`
	Storage      string     `json:"storage"`
	StorageClass string     `json:"storageClass,omitempty"`
}
type DiskSource struct {
	Type string `json:"type"` // "http" ou "blank" (minuscules)
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
				IPAddress   string   `json:"ipAddress"`
				IPAddresses []string `json:"ipAddresses"` // KubeVirt : liste, IPv6 incluse
			} `json:"interfaces"`
		} `json:"status"`
	} `json:"vmi"`
}

// CreatedByLabel : label libre accepté par l'API (préfixe user.superphenix.net/ obligatoire, format clé:valeur).
func CreatedByLabel(value string) string { return "user.superphenix.net/created-by:" + value }

// UserCloudInit génère une cloud-init personnalisée : "custom" remplace celle de Superphenix,
// qui crée l'utilisateur spx. On recrée donc l'utilisateur SSH avec sudo sans mot de passe.
func UserCloudInit(user, authorizedKey string, packages ...string) string {
	s := "#cloud-config\n"
	if len(packages) > 0 {
		s += "packages: [" + strings.Join(packages, ", ") + "]\n"
	}
	return s + "users:\n  - name: " + user + "\n    groups: sudo\n    shell: /bin/bash\n" +
		"    sudo: ['ALL=(ALL) NOPASSWD:ALL']\n    ssh_authorized_keys:\n      - " + strings.TrimSpace(authorizedKey) + "\n"
}

// GiB convertit "20Gi" ou "20" en "20" : l'API attend un entier en Gio, sans unité (max 5000).
func GiB(size string) (string, error) {
	n, err := GiBInt(size)
	return strconv.Itoa(n), err
}

// GiBInt convertit "4Gi" ou "4" en 4. L'API déployée attend compute.memory en entier de Gio
// (ponytail: les versions plus récentes de l'API attendent une quantité "4Gi", à revoir à la mise à jour).
func GiBInt(size string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSuffix(size, "Gi"))
	if err != nil || n < 1 || n > 5000 {
		return 0, fmt.Errorf("invalid size %q: expected a whole number of GiB between 1 and 5000, for example 20Gi", size)
	}
	return n, nil
}

// IP renvoie la première IPv4 connue de l'instance (ipAddresses puis ipAddress), "" sinon.
func (i *Instance) IP() string {
	for _, n := range i.VMI.Status.Interfaces {
		for _, a := range append(n.IPAddresses, n.IPAddress) {
			if ip := net.ParseIP(a); ip != nil && ip.To4() != nil {
				return a
			}
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

// --- Disque ---

type EIDResponse struct {
	EID string `json:"eid"`
}

// CreateDisk : POST /disk, la réponse contient l'eid.
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

// mountedRetry : délai entre deux essais tant que le disque est encore monté.
var mountedRetry = 5 * time.Second

// DeleteDiskIfExists ignore les 404 et réessaie (10 min max : la suppression de la VM peut prendre plusieurs minutes à se propager) tant que l'API répond "mounted" ou 5xx :
// la suppression d'une VM est asynchrone, son disque reste monté un moment.
func (c *Client) DeleteDiskIfExists(ctx context.Context, id string) error {
	deadline := time.Now().Add(10 * time.Minute)
	for {
		err := c.DeleteDisk(ctx, id)
		if err == nil || IsNotFound(err) {
			return nil
		}
		// "mounted" ou 5xx transitoire (vu juste après la suppression de la VM) : on réessaie.
		var se *StatusError
		retry := strings.Contains(err.Error(), "mounted") || (errors.As(err, &se) && se.Code >= 500)
		if !retry || time.Now().After(deadline) {
			return err
		}
		select {
		case <-ctx.Done():
			return err
		case <-time.After(mountedRetry):
		}
	}
}

// WaitDiskReady attend la fin de l'import du disque (phase "Succeeded"). Sans cela, une VM qui démarre
// avant la fin de l'import ne trouve rien de bootable et peut démarrer sur un autre disque.
func (c *Client) WaitDiskReady(ctx context.Context, id string, every time.Duration) error {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		var d struct {
			Disk struct {
				Status struct {
					Phase string `json:"phase"`
				} `json:"status"`
			} `json:"disk"`
		}
		if err := c.Call(ctx, "GET", "disk/"+id, nil, &d); err != nil {
			return err
		}
		switch d.Disk.Status.Phase {
		case "Succeeded":
			return nil
		case "Failed":
			return errors.New("disk " + id + ": import failed")
		}
		select {
		case <-ctx.Done():
			return errors.Join(ctx.Err(), errors.New("waiting for disk import"))
		case <-t.C:
		}
	}
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

// MergeUserData fusionne la cloud-config de l'utilisateur dans celle du plugin : `users` et `packages`
// sont concaténés (l'utilisateur SSH du plugin est toujours créé), les autres clés de l'utilisateur
// l'emportent. Seul le format `#cloud-config` (YAML) est accepté.
func MergeUserData(base, userData string) (string, error) {
	if strings.TrimSpace(userData) == "" {
		return base, nil
	}
	if !strings.HasPrefix(strings.TrimSpace(userData), "#cloud-config") {
		return "", fmt.Errorf("user_data must start with #cloud-config (other formats are not supported)")
	}
	var b, u map[string]any
	if err := yaml.Unmarshal([]byte(base), &b); err != nil {
		return "", err
	}
	if err := yaml.Unmarshal([]byte(userData), &u); err != nil {
		return "", fmt.Errorf("user_data: %w", err)
	}
	if u == nil {
		u = map[string]any{}
	}
	for _, k := range []string{"users", "packages"} {
		bl, _ := b[k].([]any)
		if v, ok := u[k]; ok {
			ul, isList := v.([]any)
			if !isList {
				return "", fmt.Errorf("user_data: %q must be a list", k)
			}
			u[k] = append(ul, bl...)
		} else if bl != nil {
			u[k] = bl
		}
	}
	out, err := yaml.Marshal(u)
	return "#cloud-config\n" + string(out), err
}
