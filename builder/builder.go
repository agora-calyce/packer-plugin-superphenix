//go:generate packer-sdc struct-markdown
//go:generate packer-sdc mapstructure-to-hcl2 -type Config
package builder

import (
	"context"
	"errors"
	"fmt"

	"github.com/agora-calyce/packer-plugin-superphenix/internal/client"
	"github.com/hashicorp/hcl/v2/hcldec"
	"github.com/hashicorp/packer-plugin-sdk/bootcommand"
	"github.com/hashicorp/packer-plugin-sdk/common"
	"github.com/hashicorp/packer-plugin-sdk/communicator"
	"github.com/hashicorp/packer-plugin-sdk/multistep"
	"github.com/hashicorp/packer-plugin-sdk/multistep/commonsteps"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"
	"github.com/hashicorp/packer-plugin-sdk/template/config"
	"github.com/hashicorp/packer-plugin-sdk/template/interpolate"
)

// Config is the configuration of the Superphenix builder.
type Config struct {
	common.PackerConfig    `mapstructure:",squash"`
	Comm                   communicator.Config `mapstructure:",squash"`
	commonsteps.HTTPConfig `mapstructure:",squash"`
	bootcommand.BootConfig `mapstructure:",squash"`

	// Base URL of the Superphenix API, for example `https://api.superphenix.example`.
	APIURL string `mapstructure:"api_url" required:"true"`
	// ID of the organization that owns the project.
	OrgID string `mapstructure:"org_id" required:"true"`
	// Availability zone in which the VM is built.
	AZ string `mapstructure:"az" required:"true"`
	// ID of the project in which the VM is built.
	ProjectID string `mapstructure:"project_id" required:"true"`
	// API token created from the console "API tokens" page, sent as
	// `Authorization: api-token <token>`. Pass it through a sensitive
	// variable (`PKR_VAR_token`) and never write it in the template.
	Token string `mapstructure:"token" required:"true"`

	// URL of the cloud image used to create the boot disk (an `http` disk
	// source). Exactly one of `source_url` and `iso_url` is required.
	SourceURL string `mapstructure:"source_url"`
	// URL of an installation ISO (an `http` disk source that Superphenix
	// downloads itself; local paths and checksums are not supported). The
	// VM boots an empty `disk_size` disk with the ISO as a CD-ROM, so the
	// installer must be driven with `boot_command` and `http_content` /
	// `http_directory`. The ISO is detached and deleted once the build is done.
	ISOURL string `mapstructure:"iso_url"`
	// UEFI firmware for ISO installs. Defaults to `true`: BIOS only tries the
	// hard disk, so the ISO would never boot. Windows needs EFI anyway.
	EFI *bool `mapstructure:"efi"`
	// Enable UEFI Secure Boot (ISO installs with EFI). Defaults to `false`.
	SecureBoot bool `mapstructure:"secure_boot"`
	// Add a TPM device to the VM (ISO installs). Defaults to `false`.
	TPM bool `mapstructure:"tpm"`
	// Size of the disk holding the ISO, a whole number of GiB larger than the
	// ISO. Defaults to `8Gi`.
	ISODiskSize string `mapstructure:"iso_disk_size"`
	// Number of vCPUs. Defaults to `2`.
	CPU int `mapstructure:"cpu"`
	// Amount of memory, a whole number of GiB such as `4Gi`. Defaults to `4Gi`.
	Memory string `mapstructure:"memory"`
	// Size of the boot disk, a whole number of GiB such as `20Gi`. Defaults to `20Gi`.
	DiskSize string `mapstructure:"disk_size"`
	// Storage class of the boot disk, for example `block-standard`. The
	// available classes are listed by `GET .../{az}/{project_id}/storage-class`.
	StorageClass string `mapstructure:"storage_class" required:"true"`
	// Effective ID of the subnet the VM is attached to. The machine running
	// Packer must be able to reach the VM's IP address.
	SubnetEID string `mapstructure:"subnet_eid" required:"true"`
	// Cloud-init user-data (`#cloud-config` YAML), for example
	// `file("user-data.yml")`, set on the instance's CloudInit configuration.
	// With `source_url` it is merged with the plugin's own config: `users` and
	// `packages` are appended (the SSH user is always created), other keys
	// override. With `iso_url` it is passed as is, and is the way to give an
	// installer its answer file without any network access from the VM, for
	// example an Ubuntu `autoinstall:` section (keep `autoinstall` on the
	// kernel command line). Other formats (shell script, MIME) are not supported.
	UserData string `mapstructure:"user_data"`
	// VM type (size preset), if your AZ defines some.
	VMType string `mapstructure:"vm_type"`
}

type Builder struct{ config Config }

func (b *Builder) ConfigSpec() hcldec.ObjectSpec { return b.config.FlatMapstructure().HCL2Spec() }

func (b *Builder) Prepare(raws ...interface{}) ([]string, []string, error) {
	if err := config.Decode(&b.config, &config.DecodeOpts{PluginType: "superphenix", Interpolate: true,
		InterpolateContext: &interpolate.Context{},
		InterpolateFilter:  &interpolate.RenderFilter{Exclude: []string{"boot_command", "http_content"}}}, raws...); err != nil {
		return nil, nil, err
	}
	c := &b.config
	if c.CPU == 0 {
		c.CPU = 2
	}
	if c.Memory == "" {
		c.Memory = "4Gi"
	}
	if c.DiskSize == "" {
		c.DiskSize = "20Gi"
	}
	if c.ISODiskSize == "" {
		c.ISODiskSize = "8Gi"
	}
	if c.Comm.SSHUsername == "" {
		c.Comm.SSHUsername = "spx" // user cloud-init par défaut de Superphenix
	}
	var errs []error
	for k, v := range map[string]string{"api_url": c.APIURL, "org_id": c.OrgID, "az": c.AZ,
		"project_id": c.ProjectID, "token": c.Token, "subnet_eid": c.SubnetEID, "storage_class": c.StorageClass} {
		if v == "" {
			errs = append(errs, fmt.Errorf("%s is required", k))
		}
	}
	if (c.SourceURL == "") == (c.ISOURL == "") {
		errs = append(errs, errors.New("exactly one of source_url and iso_url is required"))
	}
	if _, err := client.GiB(c.ISODiskSize); err != nil {
		errs = append(errs, fmt.Errorf("iso_disk_size: %w", err))
	}
	if c.ISOURL == "" && len(c.BootCommand) > 0 {
		errs = append(errs, errors.New("boot_command needs iso_url"))
	}
	errs = append(errs, c.HTTPConfig.Prepare(&interpolate.Context{})...)
	errs = append(errs, c.BootConfig.Prepare(&interpolate.Context{})...)
	if _, err := client.GiB(c.DiskSize); err != nil {
		errs = append(errs, fmt.Errorf("disk_size: %w", err))
	}
	if _, err := client.GiBInt(c.Memory); err != nil {
		errs = append(errs, fmt.Errorf("memory: %w", err))
	}
	if _, err := client.MergeUserData("#cloud-config\n", c.UserData); err != nil {
		errs = append(errs, err)
	}
	for _, e := range c.Comm.Prepare(&interpolate.Context{}) {
		errs = append(errs, e)
	}
	return nil, nil, errors.Join(errs...)
}

func (b *Builder) Run(ctx context.Context, ui packersdk.Ui, hook packersdk.Hook) (packersdk.Artifact, error) {
	c := &b.config
	state := new(multistep.BasicStateBag)
	state.Put("config", c)
	state.Put("client", client.New(c.APIURL, c.OrgID, c.AZ, c.ProjectID, c.Token))
	state.Put("hook", hook)
	state.Put("ui", ui)

	steps := []multistep.Step{
		&communicator.StepSSHKeyGen{CommConf: &c.Comm, SSHTemporaryKeyPair: c.Comm.SSHTemporaryKeyPair},
		commonsteps.HTTPServerFromHTTPConfig(&c.HTTPConfig),
		&stepCreate{},
		&stepBootCommand{},
		&stepWaitIP{},
		&communicator.StepConnect{Config: &c.Comm, Host: func(s multistep.StateBag) (string, error) {
			return s.Get("ip").(string), nil
		}, SSHConfig: c.Comm.SSHConfigFunc()},
		&commonsteps.StepProvision{},
		&stepStop{},
		&stepDropISO{},
	}
	r := commonsteps.NewRunner(steps, c.PackerConfig, ui)
	r.Run(ctx, state)

	if err, ok := state.GetOk("error"); ok {
		return nil, err.(error)
	}
	if _, ok := state.GetOk(multistep.StateCancelled); ok {
		return nil, errors.New("build cancelled")
	}
	instanceID, ok1 := state.Get("instance_id").(string)
	diskID, ok2 := state.Get("disk_id").(string)
	if !ok1 || !ok2 {
		return nil, errors.New("build ended before the instance was created")
	}
	return &Artifact{InstanceID: instanceID, DiskID: diskID, Client: state.Get("client").(*client.Client),
		StateData: map[string]interface{}{"generated_data": state.Get("generated_data")}}, nil
}
