//go:generate packer-sdc struct-markdown
//go:generate packer-sdc mapstructure-to-hcl2 -type Config
package postprocessor

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
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
	// Memory of the helper VM. Defaults to `2Gi`.
	HelperMemory string `mapstructure:"helper_memory"`
	// Size of the helper's boot disk. It also holds the converted qcow2, so it
	// must be larger than the image produced. Defaults to `30Gi`.
	HelperDiskSize string `mapstructure:"helper_disk_size"`
	// SSH user of the helper image. Defaults to `spx`.
	HelperUser string `mapstructure:"helper_ssh_username"`
	// Device of the exported disk inside the helper. Defaults to `/dev/vdb`.
	DiskDevice string `mapstructure:"disk_device"`
	// Storage class of the helper's boot disk. Defaults to the AZ default.
	StorageClass string `mapstructure:"storage_class"`

	// Subnet the helper is attached to.
	SubnetEID string `mapstructure:"subnet_eid" required:"true"`
	// Effective ID of a pre-allocated EIP. When set, the helper is exposed
	// through this EIP and a security group that only allows SSH from
	// `runner_cidr`. When empty, the helper's private IP is used, which
	// requires the machine running Packer to be inside the network.
	EIPEID string `mapstructure:"eip_eid"`
	// CIDR allowed to reach the helper over SSH, typically the outgoing
	// address of the CI runner as a `/32`. Required with `eip_eid`.
	RunnerCIDR string `mapstructure:"runner_cidr"`
}

type PostProcessor struct {
	config   Config
	transfer transferFunc // remplaçable dans les tests
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
	if c.DiskDevice == "" {
		c.DiskDevice = "/dev/vdb"
	}
	var errs []error
	if c.OutputPath == "" {
		errs = append(errs, errors.New("output_path is required"))
	}
	for k, v := range map[string]string{"helper_image_url": c.HelperImageURL, "api_url": c.APIURL,
		"org_id": c.OrgID, "az": c.AZ, "project_id": c.ProjectID, "token": c.Token, "subnet_eid": c.SubnetEID} {
		if v == "" {
			errs = append(errs, fmt.Errorf("%s is required", k))
		}
	}
	if c.EIPEID != "" && c.RunnerCIDR == "" {
		errs = append(errs, errors.New("eip_eid requires runner_cidr"))
	}
	if p.transfer == nil {
		p.transfer = sshFetch
	}
	return errors.Join(errs...)
}

// FileArtifact : le qcow2 récupéré sur le runner.
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
	api := client.New(c.APIURL, c.OrgID, c.AZ, c.ProjectID, c.Token)
	if err := p.export(ctx, ui, api, instanceID, diskID); err != nil {
		return nil, false, false, err
	}
	// keep=false : l'artefact source (VM supprimée) ne doit pas être conservé.
	return &FileArtifact{p.config.OutputPath}, false, false, nil
}

// export déroule la variante 3b. Chaque ressource créée est supprimée par defer (succès ou échec).
func (p *PostProcessor) export(ctx context.Context, ui packersdk.Ui, api *client.Client, instanceID, diskID string) (err error) {
	c := &p.config
	// context.Background pour le nettoyage : ctx peut être annulé.
	bg := context.Background()
	cleanup := func(what string, fn func() error) {
		if e := fn(); e != nil && !client.IsNotFound(e) {
			ui.Error(fmt.Sprintf("cleanup %s: %v", what, e))
			err = errors.Join(err, fmt.Errorf("cleanup %s: %w", what, e))
		}
	}

	// Nettoyage enregistré AVANT toute action : si unmount/suppression échoue, la VM Packer et son disque ne fuient pas.
	// LIFO : la VM est supprimée avant le disque. 404 ignoré (déjà parti).
	defer cleanup("disk", func() error { return api.DeleteDiskIfExists(bg, diskID) })
	defer cleanup("packer instance", func() error { return api.DeleteInstance(bg, instanceID) })

	// 1-2. La VM Packer est déjà arrêtée par le builder. On détache le disque, puis on supprime la VM.
	ui.Say("Detaching disk from the Packer VM...")
	if err := api.UnmountDisk(ctx, diskID); err != nil {
		return err
	}
	if err := api.DeleteInstance(ctx, instanceID); err != nil {
		return err
	}

	// 3. Helper : clé SSH éphémère, cloud-init (qemu-utils), disque Packer en 2e volume.
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
	label := fmt.Sprintf("packer-helper-%d", time.Now().Unix())
	spec := client.CreateVMInfo{
		CloudInit: &client.CloudInit{Custom: true, Config: "#cloud-config\npackages: [qemu-utils]\nssh_authorized_keys:\n  - " +
			string(ssh.MarshalAuthorizedKey(sshPub))},
		Compute: client.Compute{CPU: c.HelperCPU, Memory: c.HelperMemory},
		Disks: []client.Disk{
			{Order: 0, Disk: &client.CreateDiskInfo{General: client.DiskGeneral{
				Source: client.DiskSource{Type: "http", URL: c.HelperImageURL}, Storage: c.HelperDiskSize, StorageClass: c.StorageClass}}},
			{Order: 1, EID: diskID},
		},
		General: client.General{RunStrategy: "Always", Labels: []string{label}},
	}
	spec.Network = []client.Network{{Enabled: true, Order: 0, SubnetEID: c.SubnetEID}} // network requis par l'API
	ui.Say("Creating export helper VM...")
	var helper client.Instance
	if err := api.CreateInstance(ctx, spec, &helper); err != nil {
		return err
	}
	if helper.EID == "" {
		return errors.New("create helper: response has no eid")
	}
	// Supprimer le helper supprime aussi le disque Packer qui lui est attaché (cf. cleanup disk, 404 ignoré).
	defer cleanup("helper", func() error { return api.DeleteInstance(bg, helper.EID) })

	wctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	ip, err := api.WaitIP(wctx, helper.EID, 5*time.Second)
	if err != nil {
		return err
	}

	// 4. Exposition : EIP + SG (tcp/22 depuis runner_cidr), ou IP privée.
	host := ip
	if c.EIPEID != "" {
		sg, err := api.AllowSSH(ctx, c.SubnetEID, label, "", c.RunnerCIDR)
		if err != nil {
			return err
		}
		defer cleanup("security group", func() error { return api.DeleteSecurityGroup(bg, sg) })
		if err := api.SetEIPTarget(ctx, c.EIPEID, ip); err != nil {
			return err
		}
		defer cleanup("eip", func() error { return api.SetEIPTarget(bg, c.EIPEID, "") })
		if host, err = api.EIPAddress(ctx, c.EIPEID); err != nil {
			return err
		}
	}

	// 5. Conversion + récupération.
	ui.Say(fmt.Sprintf("Converting and downloading qcow2 from %s...", host))
	return p.transfer(ctx, ui, host, c.HelperUser, signer, c.DiskDevice, c.OutputPath)
}
