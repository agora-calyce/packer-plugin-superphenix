package main

import (
	"fmt"
	"os"

	"github.com/agora-calyce/packer-plugin-superphenix/builder"
	"github.com/agora-calyce/packer-plugin-superphenix/postprocessor"
	"github.com/hashicorp/packer-plugin-sdk/plugin"
)

func main() {
	set := plugin.NewSet()
	set.RegisterBuilder(plugin.DEFAULT_NAME, new(builder.Builder))
	set.RegisterPostProcessor("export", new(postprocessor.PostProcessor))
	if err := set.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}
