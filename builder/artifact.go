package builder

import (
	"context"
	"fmt"

	"github.com/agora-calyce/packer-plugin-superphenix/internal/client"
)

const BuilderID = "agora-calyce.superphenix"

// Artifact : instance arrêtée, prête pour l'export.
type Artifact struct {
	InstanceID string
	DiskID     string
	Client     *client.Client
	StateData  map[string]interface{}
}

func (a *Artifact) BuilderId() string { return BuilderID }
func (a *Artifact) Files() []string   { return nil }
func (a *Artifact) Id() string        { return a.InstanceID }
func (a *Artifact) String() string    { return fmt.Sprintf("Superphenix instance %s", a.InstanceID) }

// State : lu par le post-processor à travers RPC (le type concret ne survit pas au transport).
func (a *Artifact) State(name string) interface{} {
	switch name {
	case "instance_id":
		return a.InstanceID
	case "disk_id":
		return a.DiskID
	}
	return a.StateData[name]
}
func (a *Artifact) Destroy() error {
	return a.Client.DeleteInstance(context.Background(), a.InstanceID)
}
