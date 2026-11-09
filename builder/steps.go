package builder

import (
	"context"
	"fmt"
	"time"

	"github.com/agora-calyce/packer-plugin-superphenix/internal/client"
	"github.com/hashicorp/packer-plugin-sdk/multistep"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"
)

func fail(state multistep.StateBag, err error) multistep.StepAction {
	state.Put("error", err)
	state.Get("ui").(packersdk.Ui).Error(err.Error())
	return multistep.ActionHalt
}

// stepCreate creates the instance and waits for its IP. Cleanup deletes it unless the build succeeded.
type stepCreate struct{ id, diskID string }

func (s *stepCreate) Run(ctx context.Context, state multistep.StateBag) multistep.StepAction {
	c := state.Get("config").(*Config)
	api := state.Get("client").(*client.Client)
	ui := state.Get("ui").(packersdk.Ui)

	mem, err := client.GiBInt(c.Memory)
	if err != nil {
		return fail(state, err)
	}
	cloudInit, err := client.MergeUserData(client.UserCloudInit(c.Comm.SSHUsername, string(c.Comm.SSHPublicKey)), c.UserData)
	if err != nil {
		return fail(state, err)
	}
	spec := client.CreateVMInfo{
		CloudInit: &client.CloudInit{Custom: true, Bus: "virtio", Config: cloudInit},
		Compute:   client.Compute{CPU: c.CPU, Memory: mem},
		Disks:     []client.Disk{{Order: 0, Bus: "virtio", EID: ""}}, // EID filled in after the disk is created
		General:   client.General{VMType: c.VMType, RunStrategy: "Always", Labels: []string{client.CreatedByLabel("packer")}},
	}
	spec.Network = []client.Network{{Enabled: true, Order: 0, SubnetEID: c.SubnetEID}} // network required by the API

	ui.Say("Creating boot disk...")
	size, err := client.GiB(c.DiskSize)
	if err != nil {
		return fail(state, err)
	}
	diskID, err := api.CreateDisk(ctx, client.CreateDiskInfo{General: client.DiskGeneral{
		Source: client.DiskSource{Type: "http", URL: c.SourceURL}, Storage: size, StorageClass: c.StorageClass}})
	if err != nil {
		return fail(state, err)
	}
	s.diskID = diskID
	state.Put("disk_id", diskID)
	spec.Disks[0].EID = diskID

	ui.Say("Creating instance...")
	var inst client.Instance
	if err := api.CreateInstance(ctx, spec, &inst); err != nil {
		return fail(state, err)
	}
	if inst.EID == "" {
		return fail(state, fmt.Errorf("create instance: response has no eid"))
	}
	s.id = inst.EID
	state.Put("instance_id", s.id)

	ui.Say("Waiting for instance IP...")
	wctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	ip, err := api.WaitIP(wctx, s.id, 5*time.Second)
	if err != nil {
		return fail(state, err)
	}
	state.Put("ip", ip)
	return multistep.ActionContinue
}

func (s *stepCreate) Cleanup(state multistep.StateBag) {
	if s.id == "" && s.diskID == "" {
		return
	}
	_, failed := state.GetOk("error")
	_, cancelled := state.GetOk(multistep.StateCancelled)
	if !failed && !cancelled {
		return // success: the artifact keeps the instance for the post-processor
	}
	ui := state.Get("ui").(packersdk.Ui)
	api := state.Get("client").(*client.Client)
	ui.Say("Deleting instance and disk after failure...")
	// context.Background: the build ctx may already be cancelled.
	if s.id != "" {
		if err := api.DeleteInstance(context.Background(), s.id); err != nil && !client.IsNotFound(err) {
			ui.Error("cleanup instance: " + err.Error())
		}
	}
	if s.diskID != "" {
		if err := api.DeleteDiskIfExists(context.Background(), s.diskID); err != nil {
			ui.Error("cleanup disk: " + err.Error())
		}
	}
}

// stepStop cleanly stops the VM after provisioning (prerequisite for the 3b export).
type stepStop struct{}

func (stepStop) Run(ctx context.Context, state multistep.StateBag) multistep.StepAction {
	state.Get("ui").(packersdk.Ui).Say("Stopping instance...")
	api, id := state.Get("client").(*client.Client), state.Get("instance_id").(string)
	if err := api.StopInstance(ctx, id); err != nil {
		return fail(state, err)
	}
	wctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	if err := api.WaitStopped(wctx, id, 3*time.Second); err != nil {
		return fail(state, err)
	}
	return multistep.ActionContinue
}
func (stepStop) Cleanup(multistep.StateBag) {}
