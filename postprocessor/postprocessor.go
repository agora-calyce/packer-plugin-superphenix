//go:generate packer-sdc struct-markdown
//go:generate packer-sdc mapstructure-to-hcl2 -type Config
package postprocessor

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/agora-calyce/packer-plugin-superphenix/builder"
	"github.com/agora-calyce/packer-plugin-superphenix/internal/client"
	"github.com/hashicorp/hcl/v2/hcldec"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"
	"github.com/hashicorp/packer-plugin-sdk/template/config"
	"github.com/hashicorp/packer-plugin-sdk/template/interpolate"
	"golang.org/x/crypto/ssh"
)

// Config is the configuration of the Superphenix export post-processor.
type Config struct {
	// Path of the qcow2 file written on the machine running Packer.
	OutputPath string `mapstructure:"output_path" required:"true"`

	// Base URL of the Superphenix API.
	APIURL string `mapstructure:"api_url" required:"true"`
	// ID of the organization that owns the project.
	OrgID string `mapstructure:"org_id" required:"true"`
	// Availability zone, the same as the builder's.
	AZ string `mapstructure:"az" required:"true"`
	// ID of the project, the same as the builder's.
	ProjectID string `mapstructure:"project_id" required:"true"`
	// API token. Pass it through a sensitive variable.
	Token string `mapstructure:"token" required:"true"`

	// URL of a Linux cloud image used for the helper VM. It must provide
	// cloud-init and allow installing `qemu-utils` from its package manager.
	HelperImageURL string `mapstructure:"helper_image_url" required:"true"`
	// vCPUs of the helper VM. Defaults to `2`.
	HelperCPU int `mapstructure:"helper_cpu"`
	// Memory of the helper VM. Defaults to `2Gi`. Whole GiB only, between 1 and 5000: write `2Gi` or `2` (other units such as `Mi` and fractions are rejected). The plugin sends `memory` to the API as a quantity (`4Gi`), or as a bare number to a Superphenix older than 0.10.
	HelperMemory string `mapstructure:"helper_memory"`
	// Size of the helper's boot disk (whole GiB, `30Gi` or `30`). It
	// also holds the converted qcow2, so it must be larger than the image
	// produced. Defaults to `30Gi`.
	HelperDiskSize string `mapstructure:"helper_disk_size"`
	// SSH user of the helper image. Defaults to `spx`.
	HelperUser string `mapstructure:"helper_ssh_username"`
	// Device of the exported disk inside the helper, for example `/dev/vdb`.
	// Detected automatically by default: the disk that is neither the
	// helper's boot disk nor the cloud-init disk.
	DiskDevice string `mapstructure:"disk_device"`
	// Storage class of the helper's boot disk, for example `block-standard`.
	StorageClass string `mapstructure:"storage_class" required:"true"`

	// Subnet the helper is attached to. The machine running Packer must be able
	// to reach the helper's private IP address, as it does for the builder.
	SubnetEID string `mapstructure:"subnet_eid" required:"true"`
}

type PostProcessor struct {
	config   Config
	transfer transferFunc // replaceable in tests
}

func (p *PostProcessor) ConfigSpec() hcldec.ObjectSpec { return p.config.FlatMapstructure().HCL2Spec() }

func (p *PostProcessor) Configure(raws ...interface{}) error {
	if err := config.Decode(&p.config, &config.DecodeOpts{PluginType: "superphenix-export", Interpolate: true,
		InterpolateContext: &interpolate.Context{}}, raws...); err != nil {
		return err
	}
	c := &p.config
	if c.HelperCPU == 0 {
		c.HelperCPU = 2
	}
	if c.HelperMemory == "" {
		c.HelperMemory = "2Gi"
	}
	if c.HelperDiskSize == "" {
		c.HelperDiskSize = "30Gi"
	}
	if c.HelperUser == "" {
		c.HelperUser = "spx"
	}
	var errs []error
	if c.OutputPath == "" {
		errs = append(errs, errors.New("output_path is required"))
	}
	for k, v := range map[string]string{"helper_image_url": c.HelperImageURL, "api_url": c.APIURL,
		"org_id": c.OrgID, "az": c.AZ, "project_id": c.ProjectID, "token": c.Token, "subnet_eid": c.SubnetEID, "storage_class": c.StorageClass} {
		if v == "" {
			errs = append(errs, fmt.Errorf("%s is required", k))
		}
	}
	if _, err := client.GiB(c.HelperDiskSize); err != nil {
		errs = append(errs, fmt.Errorf("helper_disk_size: %w", err))
	}
	if _, err := client.GiBInt(c.HelperMemory); err != nil {
		errs = append(errs, fmt.Errorf("helper_memory: %w", err))
	}
	if p.transfer == nil {
		p.transfer = sshFetch
	}
	return errors.Join(errs...)
}

// FileArtifact: the qcow2 retrieved on the runner.
type FileArtifact struct{ Path string }

func (a *FileArtifact) BuilderId() string        { return builder.BuilderID + ".export" }
func (a *FileArtifact) Files() []string          { return []string{a.Path} }
func (a *FileArtifact) Id() string               { return a.Path }
func (a *FileArtifact) String() string           { return "qcow2 image: " + a.Path }
func (a *FileArtifact) State(string) interface{} { return nil }
func (a *FileArtifact) Destroy() error           { return nil }

func (p *PostProcessor) PostProcess(ctx context.Context, ui packersdk.Ui, a packersdk.Artifact) (packersdk.Artifact, bool, bool, error) {
	if a.BuilderId() != builder.BuilderID {
		return nil, false, false, fmt.Errorf("superphenix-export needs a superphenix artifact, got %s", a.BuilderId())
	}
	instanceID, _ := a.State("instance_id").(string)
	diskID, _ := a.State("disk_id").(string)
	if instanceID == "" || diskID == "" {
		return nil, false, false, errors.New("artifact has no instance_id/disk_id")
	}
	c := &p.config
	// Before any resource is created: failing here costs less than after the export (already long for a Windows disk).
	if err := ensureOutputDir(c.OutputPath); err != nil {
		return nil, false, false, err
	}
	api := client.New(c.APIURL, c.OrgID, c.AZ, c.ProjectID, c.Token)
	if err := p.export(ctx, ui, api, instanceID, diskID); err != nil {
		return nil, false, false, err
	}
	// keep=false: the source artifact (deleted VM) must not be kept.
	return &FileArtifact{p.config.OutputPath}, false, false, nil
}

// ensureOutputDir creates output_path's parent folder (os.Create does not).
func ensureOutputDir(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("output_path: %w", err)
	}
	return nil
}

// export runs variant 3b. Each created resource is deleted by defer (success or failure).
func (p *PostProcessor) export(ctx context.Context, ui packersdk.Ui, api *client.Client, instanceID, diskID string) (err error) {
	c := &p.config
	// context.Background for cleanup: ctx may be cancelled.
	bg := context.Background()
	cleanup := func(what string, fn func() error) {
		if e := fn(); e != nil && !client.IsNotFound(e) {
			ui.Error(fmt.Sprintf("cleanup %s: %v", what, e))
			err = errors.Join(err, fmt.Errorf("cleanup %s: %w", what, e))
		}
	}

	// Cleanup registered BEFORE any action: if unmount/deletion fails, the Packer VM and its disk do not leak.
	// LIFO: the VM is deleted before the disk. 404 ignored (already gone).
	defer cleanup("disk", func() error { return api.DeleteDiskIfExists(bg, diskID) })
	defer cleanup("packer instance", func() error { return api.DeleteInstance(bg, instanceID) })

	// 1-2. The Packer VM is already stopped by the builder. Detach the disk, then delete the VM.
	ui.Say("Detaching disk from the Packer VM...")
	if err := api.UnmountDisk(ctx, diskID); err != nil {
		return err
	}
	if err := api.DeleteInstance(ctx, instanceID); err != nil {
		return err
	}

	// 3. Helper: ephemeral SSH key, cloud-init (qemu-utils), Packer disk as 2nd volume.
	pub, priv, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		return e
	}
	signer, e := ssh.NewSignerFromKey(priv)
	if e != nil {
		return e
	}
	sshPub, e := ssh.NewPublicKey(pub)
	if e != nil {
		return e
	}
	helperSize, e := client.GiB(c.HelperDiskSize)
	if e != nil {
		return e
	}
	helperMem, e := client.GiBInt(c.HelperMemory)
	if e != nil {
		return e
	}
	// Helper boot disk created separately (like the builder): deleting the VM does not delete its disks.
	bootID, e := api.CreateDisk(ctx, client.CreateDiskInfo{General: client.DiskGeneral{
		Source: client.DiskSource{Type: "http", URL: c.HelperImageURL}, Storage: helperSize, StorageClass: c.StorageClass}})
	if e != nil {
		return e
	}
	defer cleanup("helper boot disk", func() error { return api.DeleteDiskIfExists(bg, bootID) })
	ui.Say("Waiting for the helper image import...")
	ictx, icancel := context.WithTimeout(ctx, 15*time.Minute)
	defer icancel()
	if e := api.WaitDiskReady(ictx, bootID, 5*time.Second); e != nil {
		return e
	}
	label := fmt.Sprintf("packer-helper-%d", time.Now().Unix())
	spec := client.CreateVMInfo{
		CloudInit: &client.CloudInit{Custom: true, Bus: "virtio",
			Config: client.UserCloudInit(c.HelperUser, string(ssh.MarshalAuthorizedKey(sshPub)), "qemu-utils")},
		Compute: client.Compute{CPU: c.HelperCPU, Memory: helperMem},
		Disks: []client.Disk{
			{Order: 0, Bus: "virtio", EID: bootID},
			{Order: 1, Bus: "virtio", EID: diskID},
		},
		General: client.General{RunStrategy: "Always", Labels: []string{client.CreatedByLabel(label)}},
	}
	spec.Network = []client.Network{{Enabled: true, Order: 0, SubnetEID: c.SubnetEID}} // network required by the API
	ui.Say("Creating export helper VM...")
	var helper client.Instance
	if err := api.CreateInstance(ctx, spec, &helper); err != nil {
		return err
	}
	if helper.EID == "" {
		return errors.New("create helper: response has no eid")
	}
	// Deleting the helper also deletes the Packer disk attached to it (see disk cleanup, 404 ignored).
	defer cleanup("helper", func() error { return api.DeleteInstance(bg, helper.EID) })

	wctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	ip, err := api.WaitIP(wctx, helper.EID, 5*time.Second)
	if err != nil {
		return err
	}

	// 4. Conversion + download (the runner is in the network: helper's private IP).
	ui.Say(fmt.Sprintf("Converting and downloading qcow2 from %s...", ip))
	return p.transfer(ctx, ui, ip, c.HelperUser, signer, c.DiskDevice, c.OutputPath)
}
