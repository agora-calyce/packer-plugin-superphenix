package main

import (
	"fmt"
	"os"

	"github.com/agora-calyce/packer-plugin-superphenix/builder"
	"github.com/agora-calyce/packer-plugin-superphenix/postprocessor"
	"github.com/hashicorp/packer-plugin-sdk/plugin"
	"github.com/hashicorp/packer-plugin-sdk/version"
)

// Surchargeables à la compilation : -ldflags "-X main.Version=0.1.0 -X main.VersionPrerelease="
var (
	Version           = "0.1.0"
	VersionPrerelease = "dev"
)

func main() {
	set := plugin.NewSet()
	set.SetVersion(version.NewPluginVersion(Version, VersionPrerelease, ""))
	set.RegisterBuilder(plugin.DEFAULT_NAME, new(builder.Builder))
	set.RegisterPostProcessor("export", new(postprocessor.PostProcessor))
	if err := set.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}
