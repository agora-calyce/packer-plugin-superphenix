package builder

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/packer-plugin-sdk/multistep"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"
)

const finalizeScriptPath = `C:\Windows\Temp\packer-spx-finalize.ps1`

// psq quotes s as a PowerShell single-quoted string.
func psq(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

// finalizeScript creates the final administrator account and registers a one-shot startup task (SYSTEM) that, at
// the first boot of the image, deletes the build account and turns unencrypted Basic WinRM off. Both must wait: Packer's
// own session runs as the build account over Basic. The password only lives in this script, deleted when it ends.
func finalizeScript(build, user, password string) string {
	deferred := strings.Join([]string{
		"Start-Service WinRM",
		"Set-Item WSMan:\\localhost\\Service\\AllowUnencrypted $false",
		"Set-Item WSMan:\\localhost\\Service\\Auth\\Basic $false",
		"Get-CimInstance Win32_UserProfile | Where-Object { $_.LocalPath -like '*\\' + " + psq(build) + " } | Remove-CimInstance",
		"try { Remove-LocalUser -Name " + psq(build) + " } catch { Disable-LocalUser -Name " + psq(build) + " }",
		// The logon screen keeps showing the last user (the auto-logged build account) from this cache.
		`Remove-ItemProperty 'HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Authentication\LogonUI' -Name LastLoggedOnUser, LastLoggedOnSAMUser, LastLoggedOnDisplayName, LastLoggedOnUserSID, SelectedUserSID -ErrorAction SilentlyContinue`,
		"Unregister-ScheduledTask -TaskName PackerFinalize -Confirm:$false",
	}, "; ")
	return strings.Join([]string{
		"$ErrorActionPreference = 'Stop'",
		"Remove-Item $PSCommandPath -Force", // the password must not outlive this run
		"New-LocalUser -Name " + psq(user) + " -Password (ConvertTo-SecureString " + psq(password) + " -AsPlainText -Force) -PasswordNeverExpires -AccountNeverExpires | Out-Null",
		"Add-LocalGroupMember -SID S-1-5-32-544 -Member " + psq(user),
		// Auto-logon keeps the build password in clear text in the registry.
		`$w = 'HKLM:\SOFTWARE\Microsoft\Windows NT\CurrentVersion\Winlogon'`,
		"Set-ItemProperty $w AutoAdminLogon 0",
		"Remove-ItemProperty $w DefaultPassword, DefaultUserName -ErrorAction SilentlyContinue",
		"$cmd = " + psq(deferred),
		"Register-ScheduledTask -TaskName PackerFinalize -User SYSTEM -RunLevel Highest -Force " +
			"-Trigger (New-ScheduledTaskTrigger -AtStartup) " +
			"-Action (New-ScheduledTaskAction -Execute powershell.exe -Argument ('-NoProfile -Command \"' + $cmd + '\"')) | Out-Null",
	}, "\r\n") + "\r\n"
}

// stepFinalizeWindows runs after the provisioners, before the VM is stopped (WinRM builds with winrm_final_username).
type stepFinalizeWindows struct{}

func (stepFinalizeWindows) Run(ctx context.Context, state multistep.StateBag) multistep.StepAction {
	c := state.Get("config").(*Config)
	if c.FinalUsername == "" {
		return multistep.ActionContinue
	}
	ui := state.Get("ui").(packersdk.Ui)
	comm := state.Get("communicator").(packersdk.Communicator)
	ui.Say(fmt.Sprintf("Creating account %q and scheduling the removal of build account %q...", c.FinalUsername, c.Comm.WinRMUser))

	if err := comm.Upload(finalizeScriptPath, strings.NewReader(finalizeScript(c.Comm.WinRMUser, c.FinalUsername, c.FinalPassword)), nil); err != nil {
		return fail(state, fmt.Errorf("uploading the finalize script: %w", err))
	}
	var out bytes.Buffer
	cmd := &packersdk.RemoteCmd{
		Command: `powershell -NoProfile -ExecutionPolicy Bypass -File ` + finalizeScriptPath,
		Stdout:  &out, Stderr: &out,
	}
	if err := cmd.RunWithUi(ctx, comm, ui); err != nil {
		return fail(state, fmt.Errorf("finalize script: %w", err))
	}
	if cmd.ExitStatus() != 0 {
		return fail(state, fmt.Errorf("finalize script failed (exit %d): %s", cmd.ExitStatus(), strings.TrimSpace(out.String())))
	}
	return multistep.ActionContinue
}
func (stepFinalizeWindows) Cleanup(multistep.StateBag) {}
