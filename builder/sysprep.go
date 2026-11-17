package builder

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/agora-calyce/packer-plugin-superphenix/internal/client"
	"github.com/hashicorp/packer-plugin-sdk/multistep"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"
)

// finishSteps runs once the provisioners are done. Sysprep must come after stepFinalizeWindows (it needs the WinRM
// session, which /generalize cuts by removing the network adapter) and before stepStop (it shuts the VM down itself).
func finishSteps() []multistep.Step {
	return []multistep.Step{stepFinalizeWindows{}, stepSysprepWindows{}, &stepStop{}, &stepDropISO{}}
}

// sysprepLaunch returns the PowerShell that registers and starts a one-shot SYSTEM task running Sysprep: the WinRM
// command returns at once, before the adapter disappears. The unattend path goes inside the task's argument string.
// ponytail: the (trigger-less) PackerSysprep task stays in the image, harmless; delete it at first boot if it matters.
func sysprepLaunch(unattend string) string {
	args := "/generalize /oobe /shutdown /quiet"
	if unattend != "" {
		args += ` /unattend:"` + unattend + `"`
	}
	return strings.Join([]string{
		"$ErrorActionPreference = 'Stop'",
		"Register-ScheduledTask -TaskName PackerSysprep -User SYSTEM -RunLevel Highest -Force " +
			`-Action (New-ScheduledTaskAction -Execute "$env:windir\System32\Sysprep\sysprep.exe" -Argument ` + psq(args) + ") | Out-Null",
		"Start-ScheduledTask -TaskName PackerSysprep",
	}, "; ")
}

// encodePS encodes a script for powershell -EncodedCommand (base64 of UTF-16LE): no quoting to get wrong.
func encodePS(script string) string {
	u := utf16.Encode([]rune(script))
	b := make([]byte, 0, 2*len(u))
	for _, r := range u {
		b = append(b, byte(r), byte(r>>8))
	}
	return base64.StdEncoding.EncodeToString(b)
}

// stepSysprepWindows runs Sysprep when `sysprep` is set, then waits for the shutdown it triggers.
type stepSysprepWindows struct{}

func (stepSysprepWindows) Run(ctx context.Context, state multistep.StateBag) multistep.StepAction {
	c := state.Get("config").(*Config)
	if !c.Sysprep {
		return multistep.ActionContinue
	}
	ui := state.Get("ui").(packersdk.Ui)
	comm := state.Get("communicator").(packersdk.Communicator)
	ui.Say("Running Sysprep (the VM shuts down by itself)...")

	var out bytes.Buffer
	cmd := &packersdk.RemoteCmd{
		Command: "powershell -NoProfile -ExecutionPolicy Bypass -EncodedCommand " + encodePS(sysprepLaunch(c.SysprepUnattend)),
		Stdout:  &out, Stderr: &out,
	}
	if err := cmd.RunWithUi(ctx, comm, ui); err != nil {
		return fail(state, fmt.Errorf("launching Sysprep: %w", err))
	}
	if cmd.ExitStatus() != 0 {
		return fail(state, fmt.Errorf("launching Sysprep failed (exit %d): %s", cmd.ExitStatus(), strings.TrimSpace(out.String())))
	}

	wctx, cancel := context.WithTimeout(ctx, c.SysprepTimeout)
	defer cancel()
	api, id := state.Get("client").(*client.Client), state.Get("instance_id").(string)
	if err := api.WaitStopped(wctx, id, 2*time.Second); err != nil {
		if ctx.Err() != nil {
			return fail(state, ctx.Err())
		}
		return fail(state, fmt.Errorf("Sysprep did not shut the VM down within %s (the VM has no network, check "+
			`C:\Windows\System32\Sysprep\Panther\setupact.log on it): %w`, c.SysprepTimeout, err))
	}
	return multistep.ActionContinue
}
func (stepSysprepWindows) Cleanup(multistep.StateBag) {}
