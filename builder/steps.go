package builder

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/agora-calyce/packer-plugin-superphenix/internal/client"
	"github.com/hashicorp/packer-plugin-sdk/bootcommand"
	"github.com/hashicorp/packer-plugin-sdk/multistep"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"
	"github.com/hashicorp/packer-plugin-sdk/template/interpolate"
)

func fail(state multistep.StateBag, err error) multistep.StepAction {
	state.Put("error", err)
	state.Get("ui").(packersdk.Ui).Error(err.Error())
	return multistep.ActionHalt
}

// stepCreate creates disk(s) and instance. Cleanup deletes them unless the build succeeded.
// Cloud image: one `http` disk. ISO: blank disk + ISO disk as CD-ROM, EFI (see below).
type stepCreate struct{ id, diskID, isoID, cdID string }

func (s *stepCreate) Run(ctx context.Context, state multistep.StateBag) multistep.StepAction {
	c := state.Get("config").(*Config)
	api := state.Get("client").(*client.Client)
	ui := state.Get("ui").(packersdk.Ui)

	mem, err := client.GiBInt(c.Memory)
	if err != nil {
		return fail(state, err)
	}
	spec := client.CreateVMInfo{
		Compute: client.Compute{CPU: c.CPU, Memory: mem},
		General: client.General{VMType: c.VMType, RunStrategy: "Always", Labels: []string{client.CreatedByLabel("packer")}},
	}
	spec.Network = []client.Network{{Enabled: true, Order: 0, SubnetEID: c.SubnetEID}} // network required by the API

	size, err := client.GiB(c.DiskSize)
	if err != nil {
		return fail(state, err)
	}
	newDisk := func(src client.DiskSource, size string) (string, error) {
		return api.CreateDisk(ctx, client.CreateDiskInfo{General: client.DiskGeneral{
			Source: src, Storage: size, StorageClass: c.StorageClass, Labels: []string{client.CreatedByLabel("packer")}}})
	}

	if c.ISOURL == "" {
		cloudInit, err := client.MergeUserData(client.UserCloudInit(c.Comm.SSHUsername, string(c.Comm.SSHPublicKey)), c.UserData)
		if err != nil {
			return fail(state, err)
		}
		spec.CloudInit = &client.CloudInit{Custom: true, Bus: "virtio", Config: cloudInit}
		ui.Say("Creating boot disk...")
		if s.diskID, err = newDisk(client.DiskSource{Type: "http", URL: c.SourceURL}, size); err != nil {
			return fail(state, err)
		}
		spec.Disks = []client.Disk{{Order: 0, Bus: "virtio", EID: s.diskID}}
	} else {
		if c.UserData != "" {
			// Instance cloud-init disk: read by the installer (e.g. Ubuntu autoinstall), no network needed.
			// The content is passed as is (no merge: the installer defines its own users).
			spec.CloudInit = &client.CloudInit{Custom: true, Bus: "virtio", Config: c.UserData}
		}
		ui.Say("Creating empty disk and ISO disk...")
		if s.diskID, err = newDisk(client.DiskSource{Type: "blank"}, size); err != nil {
			return fail(state, err)
		}
		isoSize, _ := client.GiB(c.ISODiskSize)
		if s.isoID, err = newDisk(client.DiskSource{Type: "http", URL: c.ISOURL}, isoSize); err != nil {
			return fail(state, err)
		}
		drop := []string{s.isoID}
		spec.Disks = []client.Disk{{Order: 0, Bus: c.DiskBus, EID: s.diskID}, {Order: 1, Bus: "sata", CDROM: true, EID: s.isoID}}
		cdURL, cdSize := c.CDURL, ""
		if cdURL != "" {
			cdSize, _ = client.GiB(c.CDDiskSize)
		} else if cdPath, ok := state.Get("cd_path").(string); ok {
			// Generated ISO (cd_files/cd_content), imported from Packer's HTTP server.
			st, err := os.Stat(cdPath)
			if err != nil {
				return fail(state, err)
			}
			ip, err := outboundIP(c.APIURL)
			if err != nil {
				return fail(state, err)
			}
			port, _ := state.Get("http_port").(int)
			cdURL = fmt.Sprintf("http://%s:%d/%s", ip, port, cdFileName)
			cdSize = fmt.Sprint(st.Size()/(1<<30) + 1) // whole GiB, no unit
		}
		if cdURL != "" {
			ui.Say("Creating CD disk...")
			if s.cdID, err = newDisk(client.DiskSource{Type: "http", URL: cdURL}, cdSize); err != nil {
				return fail(state, err)
			}
			drop = append(drop, s.cdID)
			spec.Disks = append(spec.Disks, client.Disk{Order: 2, Bus: "sata", CDROM: true, EID: s.cdID})
		}
		state.Put("drop_disks", drop)
		// With a vm_type (e.g. windows.2k25) and no explicit option, the type sets EFI/Secure Boot/TPM.
		if c.VMType == "" || c.EFI != nil || c.SecureBoot != nil || c.TPM != nil {
			adv := &client.Advanced{}
			// BIOS (SeaBIOS) only tries the hard disk: the CD-ROM is only booted with EFI. Tested: empty disk
			// first (not bootable, EFI falls through to the CD), then the installed disk boots on its own after reboot.
			// An explicit efi = false is sent as such, otherwise the vm_type's own firmware would apply.
			efi := c.EFI == nil || *c.EFI
			adv.Firmware = &client.Firmware{Bootloader: client.Bootloader{EFI: client.EFI{Enabled: efi, SecureBoot: efi && c.SecureBoot != nil && *c.SecureBoot}}}
			if c.TPM != nil {
				adv.Devices = &client.Devices{TPM: client.TPM{Enabled: *c.TPM}}
			}
			spec.Advanced = adv
		}
	}
	state.Put("disk_id", s.diskID)

	// Both flows: a VM created before its disks are imported finds nothing bootable (see DESIGN.md).
	ui.Say("Waiting for disk import...")
	drop, _ := state.Get("drop_disks").([]string)
	for _, id := range append([]string{s.diskID}, drop...) {
		wctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
		err := api.WaitDiskReady(wctx, id, 5*time.Second)
		cancel()
		if err != nil {
			return fail(state, err)
		}
	}

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
	return multistep.ActionContinue
}

func (s *stepCreate) Cleanup(state multistep.StateBag) {
	if s.id == "" && s.diskID == "" && s.isoID == "" && s.cdID == "" {
		return
	}
	// Anything but a completed build (flag set by stepDropISO, the last step) deletes the resources:
	// do not rely on StateCancelled, set asynchronously by the runner and sometimes not yet visible here.
	if _, ok := state.GetOk("build_ok"); ok {
		return // success: the artifact keeps the instance for the post-processor
	}
	ui := state.Get("ui").(packersdk.Ui)
	api := state.Get("client").(*client.Client)
	ui.Say("Deleting instance and disks after failure...")
	// context.Background: the build ctx may already be cancelled.
	if s.id != "" {
		if err := api.DeleteInstance(context.Background(), s.id); err != nil && !client.IsNotFound(err) {
			// The VM is still there, so its disks stay mounted: waiting to delete them would only burn minutes.
			ui.Error(fmt.Sprintf("cleanup instance: %v (instance %s and disks %v left behind, delete them by hand)",
				err, s.id, []string{s.diskID, s.isoID, s.cdID}))
			return
		}
	}
	for _, d := range []string{s.diskID, s.isoID, s.cdID} {
		if d == "" {
			continue
		}
		if err := api.DeleteDiskIfExists(context.Background(), d); err != nil {
			ui.Error("cleanup disk: " + err.Error())
		}
	}
}

// stepBootCommand types boot_command on the VNC console after boot_wait (ISO installer only).
type stepBootCommand struct{}

func (stepBootCommand) Run(ctx context.Context, state multistep.StateBag) multistep.StepAction {
	c := state.Get("config").(*Config)
	if len(c.BootCommand) == 0 {
		return multistep.ActionContinue
	}
	api, ui := state.Get("client").(*client.Client), state.Get("ui").(packersdk.Ui)
	id := state.Get("instance_id").(string)

	httpIP, err := outboundIP(c.APIURL)
	if err != nil {
		return fail(state, err)
	}
	httpPort, _ := state.Get("http_port").(int)
	ictx := interpolate.Context{Data: &struct {
		HTTPIP   string
		HTTPPort int
		Name     string
	}{httpIP, httpPort, "superphenix"}}

	ui.Say(fmt.Sprintf("Waiting %s before typing the boot command...", c.BootWait))
	select {
	case <-time.After(c.BootWait):
	case <-ctx.Done():
		return fail(state, ctx.Err())
	}
	var vnc *client.VNC
	for deadline := time.Now().Add(2 * time.Minute); ; time.Sleep(3 * time.Second) {
		if vnc, err = api.DialVNC(ctx, id); err == nil {
			break
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			return fail(state, err)
		}
	}
	defer vnc.Close()

	ui.Say("Typing the boot command...")
	d := bootcommand.NewVNCDriver(vnc, 0)
	for _, cmd := range c.BootCommand {
		cmd, err := interpolate.Render(cmd, &ictx)
		if err != nil {
			return fail(state, err)
		}
		seq, err := bootcommand.GenerateExpressionSequence(cmd)
		if err != nil {
			return fail(state, err)
		}
		if err := seq.Do(ctx, d); err != nil {
			return fail(state, err)
		}
	}
	return multistep.ActionContinue
}
func (stepBootCommand) Cleanup(multistep.StateBag) {}

// outboundIP: local address used to reach the API, hence inside the subnet when the runner is in it.
// ponytail: assumes the VM reaches the runner through the same interface as the API; add an http_ip option if needed.
func outboundIP(apiURL string) (string, error) {
	u, err := url.Parse(apiURL)
	if err != nil {
		return "", err
	}
	port := u.Port()
	if port == "" {
		port = map[string]string{"http": "80"}[u.Scheme]
		if port == "" {
			port = "443"
		}
	}
	conn, err := net.Dial("udp4", net.JoinHostPort(u.Hostname(), port)) // IPv4: the VM only reports IPv4, and HTTPIP:HTTPPort needs no brackets
	if err != nil {
		return "", err
	}
	defer conn.Close()
	return conn.LocalAddr().(*net.UDPAddr).IP.String(), nil
}

// stepWaitIP waits until the instance is Running with an IP (installer or booted system).
type stepWaitIP struct{}

func (stepWaitIP) Run(ctx context.Context, state multistep.StateBag) multistep.StepAction {
	state.Get("ui").(packersdk.Ui).Say("Waiting for instance IP...")
	wctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	ip, err := state.Get("client").(*client.Client).WaitIP(wctx, state.Get("instance_id").(string), 5*time.Second)
	if err != nil {
		return fail(state, err)
	}
	state.Put("ip", ip)
	return multistep.ActionContinue
}
func (stepWaitIP) Cleanup(multistep.StateBag) {}

// stepStop cleanly stops the VM after provisioning (prerequisite for the export).
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

// stepStageCD exposes the generated ISO (cd_path) in the folder served by the HTTP server.
type stepStageCD struct{ dir string }

const cdFileName = "cd.iso"

func (s *stepStageCD) Run(_ context.Context, state multistep.StateBag) multistep.StepAction {
	p, ok := state.Get("cd_path").(string)
	if !ok {
		return fail(state, errors.New("cd_files/cd_content: no CD was created"))
	}
	if err := os.Symlink(p, filepath.Join(s.dir, cdFileName)); err != nil {
		return fail(state, err)
	}
	return multistep.ActionContinue
}
func (*stepStageCD) Cleanup(multistep.StateBag) {}

// stepDropISO detaches and deletes the ISO/CD disks once the VM is stopped (they are not part of the artifact).
type stepDropISO struct{}

func (stepDropISO) Run(ctx context.Context, state multistep.StateBag) multistep.StepAction {
	ids, _ := state.Get("drop_disks").([]string)
	api := state.Get("client").(*client.Client)
	for _, id := range ids {
		state.Get("ui").(packersdk.Ui).Say("Removing ISO disk " + id + "...")
		if err := api.UnmountDisk(ctx, id); err != nil {
			return fail(state, err)
		}
		if err := api.DeleteDiskIfExists(ctx, id); err != nil {
			return fail(state, err)
		}
	}
	state.Put("build_ok", true)
	return multistep.ActionContinue
}
func (stepDropISO) Cleanup(multistep.StateBag) {}
