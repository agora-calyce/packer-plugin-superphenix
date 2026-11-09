package builder

import (
	"context"
	"fmt"

	"github.com/agora-calyce/packer-plugin-superphenix/internal/client"
)

const BuilderID = "agora-calyce.superphenix"

// Artifact: stopped instance, ready for export.
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

// State: read by the post-processor over RPC (the concrete type does not survive transport).
func (a *Artifact) State(name string) interface{} {
	switch name {
	case "instance_id":
		return a.InstanceID
	case "disk_id":
		return a.DiskID
	}
	return a.StateData[name]
}

// Destroy deletes the VM then the disk; 404 ignored (the post-processor may have already deleted everything).
func (a *Artifact) Destroy() error {
	ctx := context.Background()
	if err := a.Client.DeleteInstance(ctx, a.InstanceID); err != nil && !client.IsNotFound(err) {
		return err
	}
	return a.Client.DeleteDiskIfExists(ctx, a.DiskID)
}
