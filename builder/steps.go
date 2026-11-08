package builder

import (
	"context"
	"fmt"
	"net"
	"net/url"
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

// stepCreate crée disque(s) et instance. Cleanup les supprime tant que le build n'a pas réussi.
// Image cloud : un disque `http`. ISO : disque vide + disque ISO en CD-ROM, EFI (voir ci-dessous).
type stepCreate struct{ id, diskID, isoID string }

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
	spec.Network = []client.Network{{Enabled: true, Order: 0, SubnetEID: c.SubnetEID}} // network requis par l'API

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
			// Disque cloud-init de l'instance : lu par l'installeur (ex. autoinstall Ubuntu), sans réseau.
			// Le contenu est passé tel quel (pas de fusion : l'installeur définit lui-même ses utilisateurs).
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
		state.Put("iso_disk_id", s.isoID)
		for _, id := range []string{s.diskID, s.isoID} {
			wctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
			err := api.WaitDiskReady(wctx, id, 5*time.Second)
			cancel()
			if err != nil {
				return fail(state, err)
			}
		}
		spec.Disks = []client.Disk{{Order: 0, Bus: "virtio", EID: s.diskID}, {Order: 1, Bus: "sata", CDROM: true, EID: s.isoID}}
		adv := &client.Advanced{}
		if c.EFI == nil || *c.EFI {
			// BIOS (SeaBIOS) n'essaie que le disque dur : le CD-ROM n'est amorcé qu'en EFI. Testé : disque vide
			// d'abord (non amorçable, l'EFI passe au CD), puis le disque installé démarre seul au reboot.
			adv.Firmware = &client.Firmware{Bootloader: client.Bootloader{EFI: client.EFI{Enabled: true, SecureBoot: c.SecureBoot}}}
		}
		if c.TPM {
			adv.Devices = &client.Devices{TPM: client.TPM{Enabled: true}}
		}
		if adv.Firmware != nil || adv.Devices != nil {
			spec.Advanced = adv
		}
	}
	state.Put("disk_id", s.diskID)

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
	if s.id == "" && s.diskID == "" && s.isoID == "" {
		return
	}
	// Tout sauf un build abouti (flag posé par stepDropISO, dernière étape) supprime les ressources :
	// ne pas se fier à StateCancelled, posé en asynchrone par le runner et parfois pas encore visible ici.
	if _, ok := state.GetOk("build_ok"); ok {
		return // succès : l'artefact garde l'instance pour le post-processor
	}
	ui := state.Get("ui").(packersdk.Ui)
	api := state.Get("client").(*client.Client)
	ui.Say("Deleting instance and disks after failure...")
	// context.Background : le ctx du build est peut-être déjà annulé.
	if s.id != "" {
		if err := api.DeleteInstance(context.Background(), s.id); err != nil && !client.IsNotFound(err) {
			ui.Error("cleanup instance: " + err.Error())
		}
	}
	for _, d := range []string{s.diskID, s.isoID} {
		if d == "" {
			continue
		}
		if err := api.DeleteDiskIfExists(context.Background(), d); err != nil {
			ui.Error("cleanup disk: " + err.Error())
		}
	}
}

// stepBootCommand tape boot_command sur la console VNC après boot_wait (installeur ISO seulement).
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
		return multistep.ActionHalt
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

// outboundIP : adresse locale utilisée pour joindre l'API, donc dans le subnet quand le runner y est.
// ponytail: suppose que la VM joint le runner par la même interface que l'API ; une option http_ip si besoin.
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
	conn, err := net.Dial("udp", net.JoinHostPort(u.Hostname(), port))
	if err != nil {
		return "", err
	}
	defer conn.Close()
	return conn.LocalAddr().(*net.UDPAddr).IP.String(), nil
}

// stepWaitIP attend que l'instance soit Running avec une IP (installeur ou système démarré).
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

// stepStop arrête proprement la VM après provisioning (prérequis de l'export 3b).
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

// stepDropISO détache et supprime le disque ISO une fois la VM arrêtée (il n'appartient pas à l'artefact).
type stepDropISO struct{}

func (stepDropISO) Run(ctx context.Context, state multistep.StateBag) multistep.StepAction {
	id, ok := state.Get("iso_disk_id").(string)
	if !ok {
		state.Put("build_ok", true)
		return multistep.ActionContinue
	}
	api := state.Get("client").(*client.Client)
	state.Get("ui").(packersdk.Ui).Say("Removing ISO disk...")
	if err := api.UnmountDisk(ctx, id); err != nil {
		return fail(state, err)
	}
	if err := api.DeleteDiskIfExists(ctx, id); err != nil {
		return fail(state, err)
	}
	state.Put("build_ok", true)
	return multistep.ActionContinue
}
func (stepDropISO) Cleanup(multistep.StateBag) {}
