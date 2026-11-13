//go:generate packer-sdc struct-markdown
//go:generate packer-sdc mapstructure-to-hcl2 -type Config
package builder

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

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
	commonsteps.CDConfig   `mapstructure:",squash"`
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
	// Enable UEFI Secure Boot (ISO installs with EFI). Defaults to `false`,
	// or to what `vm_type` defines when `vm_type` is set and none of `efi`,
	// `secure_boot` and `tpm` is.
	SecureBoot *bool `mapstructure:"secure_boot"`
	// Add a TPM device to the VM (ISO installs). Defaults to `false`, or to
	// what `vm_type` defines (see `secure_boot`).
	TPM *bool `mapstructure:"tpm"`
	// URL of a ready-made ISO (an `http` disk source that Superphenix downloads
	// itself) attached as a second CD-ROM, typically an answer-file ISO
	// (`autounattend.xml`) for Windows. Needs `iso_url`; exclusive with
	// `cd_files` and `cd_content`. Use it when Superphenix cannot reach the
	// Packer machine to import a generated ISO.
	CDURL string `mapstructure:"cd_url"`
	// Size of the `cd_url` disk, larger than the ISO. Defaults to `1Gi`. Whole GiB only, between 1 and 5000: write `1Gi` or `1` (other units such as `Mi` and fractions are rejected; the plugin sends the bare number to the API).
	CDDiskSize string `mapstructure:"cd_disk_size"`
	// Bus of the system disk of an ISO install: `virtio` (default) or `sata`.
	// Windows has no virtio storage driver during setup, so use `sata` for it.
	DiskBus string `mapstructure:"disk_bus"`
	// Size of the disk holding the ISO, larger than the ISO. Defaults to `8Gi`. Whole GiB only, between 1 and 5000: write `8Gi` or `8` (other units such as `Mi` and fractions are rejected; the plugin sends the bare number to the API).
	ISODiskSize string `mapstructure:"iso_disk_size"`
	// Number of vCPUs. Defaults to `2`.
	CPU int `mapstructure:"cpu"`
	// Amount of memory. Defaults to `4Gi`. Whole GiB only, between 1 and 5000: write `4Gi` or `4` (other units such as `Mi` and fractions are rejected). The plugin sends `memory` to the API as a quantity (`4Gi`), or as a bare number to a Superphenix older than 0.10.
	Memory string `mapstructure:"memory"`
	// Size of the boot disk. Defaults to `20Gi`. Whole GiB only, between 1 and 5000: write `20Gi` or `20` (other units such as `Mi` and fractions are rejected; the plugin sends the bare number to the API).
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
	// WinRM builds: name of the administrator account the image keeps, set up once the provisioners are done. The
	// build account (`winrm_username`, created by the answer file) is then deleted at the first boot of the image.
	// If the name is the built-in administrator (`Administrator`, `Administrateur`...), that account gets
	// `winrm_final_password` and is enabled; otherwise the account is created, and the built-in administrator,
	// which the answer file gave the build password, is disabled with a random password. Needs
	// `winrm_final_password`. Leave both empty to keep the build account.
	FinalUsername string `mapstructure:"winrm_final_username"`
	// Password of `winrm_final_username`. Pass it through a sensitive variable. It is never written to the
	// image in clear text and is masked in the logs.
	FinalPassword string `mapstructure:"winrm_final_password"`
}

func (c *Config) hasCD() bool { return len(c.CDFiles) > 0 || len(c.CDContent) > 0 }

type Builder struct{ config Config }

func (b *Builder) ConfigSpec() hcldec.ObjectSpec { return b.config.FlatMapstructure().HCL2Spec() }

func (b *Builder) Prepare(raws ...interface{}) ([]string, []string, error) {
	if err := config.Decode(&b.config, &config.DecodeOpts{PluginType: "superphenix", Interpolate: true,
		InterpolateContext: &interpolate.Context{},
		InterpolateFilter:  &interpolate.RenderFilter{Exclude: []string{"boot_command", "http_content", "cd_content"}}}, raws...); err != nil {
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
	if c.CDDiskSize == "" {
		c.CDDiskSize = "1Gi"
	}
	if c.DiskBus == "" {
		c.DiskBus = "virtio"
	}
	if c.ISODiskSize == "" {
		c.ISODiskSize = "8Gi"
	}
	if c.Comm.SSHUsername == "" && c.Comm.Type != "winrm" {
		c.Comm.SSHUsername = "spx" // Superphenix's default cloud-init user
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
	errs = append(errs, c.HTTPConfig.Prepare(&interpolate.Context{})...)
	errs = append(errs, c.CDConfig.Prepare(&interpolate.Context{})...)
	if c.hasCD() && c.ISOURL == "" {
		errs = append(errs, errors.New("cd_files and cd_content need iso_url"))
	}
	if c.CDURL != "" && (c.ISOURL == "" || c.hasCD()) {
		errs = append(errs, errors.New("cd_url needs iso_url and cannot be combined with cd_files/cd_content"))
	}
	if c.SecureBoot != nil && *c.SecureBoot && c.EFI != nil && !*c.EFI {
		errs = append(errs, errors.New("secure_boot needs efi"))
	}
	if _, err := client.GiB(c.CDDiskSize); err != nil {
		errs = append(errs, fmt.Errorf("cd_disk_size: %w", err))
	}
	if c.hasCD() && (c.HTTPDir != "" || len(c.HTTPContent) > 0) {
		errs = append(errs, errors.New("cd_files/cd_content cannot be combined with http_directory/http_content"))
	}
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
	if (c.FinalUsername == "") != (c.FinalPassword == "") {
		errs = append(errs, errors.New("winrm_final_username and winrm_final_password go together"))
	}
	if c.FinalUsername != "" {
		if c.Comm.Type != "winrm" {
			errs = append(errs, errors.New("winrm_final_username needs communicator = \"winrm\""))
		}
		if c.FinalUsername == c.Comm.WinRMUser {
			errs = append(errs, errors.New("winrm_final_username must differ from winrm_username (the build account is deleted)"))
		}
		packersdk.LogSecretFilter.Set(c.FinalPassword)
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

	var steps []multistep.Step
	if c.Comm.Type != "winrm" {
		steps = append(steps, &communicator.StepSSHKeyGen{CommConf: &c.Comm, SSHTemporaryKeyPair: c.Comm.SSHTemporaryKeyPair})
	}
	if c.hasCD() {
		// The generated ISO is served by Packer's HTTP server from a temporary folder, then imported by Superphenix.
		dir, err := os.MkdirTemp("", "packer-spx-cd-")
		if err != nil {
			return nil, err
		}
		defer os.RemoveAll(dir)
		c.HTTPDir = dir
		steps = append(steps,
			&commonsteps.StepCreateCD{Files: c.CDFiles, Content: c.CDContent, Label: c.CDLabel},
			&stepStageCD{dir: dir})
	}
	if c.HTTPDir != "" || len(c.HTTPContent) > 0 {
		if err := checkCacheDir(); err != nil {
			return nil, err
		}
	}
	steps = append(steps,
		commonsteps.HTTPServerFromHTTPConfig(&c.HTTPConfig),
		&stepCreate{},
		&stepBootCommand{},
		&stepWaitIP{},
		&communicator.StepConnect{Config: &c.Comm, Host: communicator.CommHost(c.Comm.Host(), "ip"), SSHConfig: c.Comm.SSHConfigFunc(),
			WinRMConfig: func(multistep.StateBag) (*communicator.WinRMConfig, error) {
				return &communicator.WinRMConfig{Username: c.Comm.WinRMUser, Password: c.Comm.WinRMPassword}, nil
			}},
		&commonsteps.StepProvision{},
		stepFinalizeWindows{},
		&stepStop{},
		&stepDropISO{},
	)
	r := commonsteps.NewRunner(steps, c.PackerConfig, ui)
	r.Run(ctx, state)

	if err, ok := state.GetOk("error"); ok {
		return nil, err.(error)
	}
	// build_ok is set by the last step. Without it stepCreate.Cleanup has deleted everything: never return an
	// artifact then, even when a halted step left no error (StateCancelled is set asynchronously by the runner).
	if _, ok := state.GetOk("build_ok"); !ok {
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

// checkCacheDir fails fast when Packer's cache directory (PACKER_CACHE_DIR) cannot be created. The SDK's HTTP
// server step otherwise retries opening its port lock file in a tight loop, forever, flooding the log.
func checkCacheDir() error {
	p, err := packersdk.CachePath("port", "check")
	if err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Dir(p)); err != nil {
		return fmt.Errorf("the Packer cache directory is unusable (check PACKER_CACHE_DIR and its permissions): %w", err)
	}
	return nil
}
