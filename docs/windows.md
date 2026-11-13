# Building Windows images

This guide builds a Windows image from a neutral installation ISO: unattended install, WinRM, your
provisioners, then (optionally) the qcow2 export. It is validated with **Windows Server 2025 (French ISO)**:
a full build takes about 37 minutes, about 43 with the qcow2 export (12 GB file for a 50 GiB disk).

Reference templates are in [`example/windows/`](../example/windows/). For Linux, see [linux.md](linux.md).

## How it works

A Windows installer is unattended with an `autounattend.xml` found at the root of any CD-ROM. Two constraints
shape the design on Superphenix:

1. Superphenix attaches a disk only if it can import it from a **URL that Superphenix itself can reach**. The
   pod that imports the disk has no route to a private IP such as the Packer runner's, so an ISO generated
   on the runner (`cd_files` / `cd_content`) cannot be imported.
2. The Windows installer (WinPE) has **no `curl`, `certutil` or PowerShell**, so it cannot download its answer
   file, and the keyboard follows the ISO language (AZERTY on a French ISO), which makes typing files over the
   VNC console impractical.

So the build uses two ISOs, and nothing from the runner is needed to start it:

```
Windows ISO (iso_url)  ─┐  imported by Superphenix from a public URL
Answer-file ISO (cd_url)┘  static autounattend.xml, imported from a URL you host once
        │ installs Windows, creates the build account, logs in
        ▼
first logon: PowerShell runs windows-initialisation.ps1, which sits on the answer-file ISO, and enables WinRM
        ▼
Packer connects over WinRM (runner to VM), runs the provisioners, then the plugin creates your final
account (winrm_final_username) and schedules the removal of the build account, then stops the VM
```

The answer-file ISO does not depend on the runner: no IP, no port, no security group rule. The same ISO serves
any runner. Everything that changes between builds (scripts, provisioners, the final account) goes over WinRM.
You rebuild and re-host the ISO only when the image, the language, the time zone or the build account change.

## Prerequisites

- Packer runs on a VM **inside the subnet** (or behind a VPN), as for any build.
- Free disk space on the runner for the exported qcow2 (about 13 GB for Windows Server 2025), if you use the
  export.
- A Windows ISO that Superphenix can download (`iso_url`).
- A place to host the answer-file ISO (step 2).
- `xorriso` and `python3` on the machine that generates the answer-file ISO (`apt install xorriso`).

## Step 1: build the answer-file ISO

`example/windows/answer-iso/make-answer-iso.sh` renders `autounattend.xml` from a template and packs it into a
small ISO (a few hundred KB):

```sh
LOCALE=fr-FR \
IMAGE_VALUE='Windows Server 2025 SERVERSTANDARD' \
sh example/windows/answer-iso/make-answer-iso.sh answers-win2025-fr.iso
```

| Variable | Default | Meaning |
|---|---|---|
| `PASSWORD` | `Packer-build-1` | Password of the build account, and also of the built-in administrator. A throwaway: the same value as `winrm_password` in the template (its default). See [The final account](#the-final-account). |
| `WIN_USERNAME` | `packer` | Build account, a local administrator that does not depend on the language. Must match `winrm_username`. |
| `LOCALE` | `fr-FR` | Language and keyboard of the installer. Must be one the Windows ISO contains. |
| `TIMEZONE` | `Romance Standard Time` | Windows time zone name. |
| `WIN_HOSTNAME` | `windows-build` | Computer name. |
| `IMAGE_KEY` / `IMAGE_VALUE` | `/IMAGE/NAME`, `Windows Server 2025 SERVERSTANDARD` | Edition to install. Use `IMAGE_KEY=/IMAGE/INDEX` with `IMAGE_VALUE=2` to select by index. |
| `PRODUCT_KEY` | empty | Product key, if the ISO needs one (an evaluation ISO does not). |

To list the editions of an ISO, mount it and run `dism /Get-WimInfo /WimFile:D:\sources\install.wim` on
Windows, or `wiminfo install.wim` from `wimtools` on Linux.

Every variable is optional. The ISO holds the build password in clear text, which is why it is a throwaway
default and not a secret. Set `winrm_final_username` / `winrm_final_password` in the template (step 3): once the
provisioners are done the plugin sets up that account, deletes the build account (and its auto-logon password) at the
first boot of the image, and disables the built-in administrator if you chose another name (details in
[The final account](#the-final-account)). Never put a real password in the ISO.

You need one ISO per combination of image, language, time zone and build account, not per runner. A different
Windows version usually only changes `IMAGE_VALUE` (or the index).

### Validate the answer file

`validate-answer-iso.py` checks an `autounattend.xml`, or an ISO that contains one, before you host it. It needs
only Python 3 (and `xorriso` to read an ISO):

```sh
WINRM_PASSWORD='Packer-build-1' \
  python3 example/windows/answer-iso/validate-answer-iso.py answers-win2025-fr.iso \
  --username packer
```

It fails (exit status 1) when:

- `autounattend.xml` is not at the root of the ISO, or the XML is not well-formed, or placeholders such as
  `@PASSWORD@` were left unreplaced;
- the EULA is not accepted, no image to install is set, or the disk has no EFI partition (the VM boots with UEFI);
- there is no build account, no auto-logon, or no `FirstLogonCommands` (nothing would enable WinRM);
- `--username` / `WINRM_PASSWORD` do not match the account in the file (they must equal `winrm_username` /
  `winrm_password` in your template).

It warns about clear-text passwords and a `FirstLogonCommands` that never mentions WinRM.
It is a structural check: it cannot prove that Windows accepts the file, only a build does.

### Bring your own ISO

You do not have to use `make-answer-iso.sh`: any ISO that follows these rules works as `cd_url`. A rendered sample
to start from is [`example/windows/answer-iso/autounattend.example.xml`](../example/windows/answer-iso/autounattend.example.xml).

1. `autounattend.xml` sits at the **root** of the ISO, with that exact name (a plain ISO 9660 with Joliet,
   for example `xorriso -as genisoimage -V ANSWERS -J -joliet-long -r -o answers.iso dir/`). A volume label is
   not required.
2. It installs unattended on a **UEFI** disk (EFI + MSR + OS partitions, see `autounattend.xml.in`) and
   selects the edition, accepts the EULA and skips the first-run screens.
3. It creates the **build account** and enables **auto-logon**. Its name and password are the template's
   `winrm_username` and `winrm_password`.
4. A `FirstLogonCommand` enables **WinRM** over HTTP (port 5985, basic auth, unencrypted) and opens the
   firewall. Put the script on your ISO and run it from the CD at first logon (the CD letter varies: look for
   the file on every drive, as `autounattend.xml.in` does). `example/windows/scripts/windows-initialisation.ps1`
   is a working script. The build VM then never needs to reach the runner.
   This setting must not reach the image. With `winrm_final_username` the plugin registers a one-shot startup
   task that turns unencrypted Basic authentication off at the first boot of a deployed VM (doing it during the
   build would cut Packer's own session). Without it, `example/windows/scripts/windows-cleanup.ps1` does the
   same: keep it as the last provisioner.
5. Run `validate-answer-iso.py` on it, host it where Superphenix can download it (step 2), and set
   `cd_url` to that link. `iso_url` stays the Windows ISO. Raise `cd_disk_size` (default `1Gi`) only if your ISO
   is larger than 1 GiB.

## Step 2: host the ISO

Superphenix must be able to download it: `http://` or `https://`, a direct link, a `Content-Length`
header, reachable from the Internet (or at least from Superphenix). Any of these works:

- An existing web server, object storage bucket or file share you control. Use a random path.
- A small VM on Superphenix, as follows.

### A small web server on Superphenix

1. **VM.** Create a VM from an Ubuntu cloud image in your subnet (1 vCPU, 1 GiB, 10 GiB is plenty) with this
   cloud-init, and your own SSH key for the `spx` user:

   ```yaml
   #cloud-config
   packages: [nginx]
   runcmd:
     - [systemctl, enable, --now, nginx]
   ```

2. **Security group.** Target this VM only: TCP 80 in from `0.0.0.0/0`, and TCP 22 in from the runner. (Through
   the API, set `endPort` equal to `port`, or the creation fails with a 500.)
3. **EIP.** Create an EIP in floating-IP mode (internal IP = the VM's private IP). Do not reuse the EIP that
   gives you SSH access to the runner.
4. **Upload.** From the runner (private network):

   ```sh
   T=$(openssl rand -hex 16)
   ssh spx@<vm ip> "sudo mkdir -p /var/www/html/$T"
   scp answers-win2025-fr.iso spx@<vm ip>:/tmp/answers.iso
   ssh spx@<vm ip> "sudo mv /tmp/answers.iso /var/www/html/$T/ && sudo chmod 644 /var/www/html/$T/answers.iso"
   echo "http://<EIP>/$T/answers.iso"
   ```

5. **Check from outside** the subnet: `curl -sI http://<EIP>/$T/answers.iso` must return `200` and a
   `Content-Length`.

The VM and EIP can be deleted once the ISO is no longer needed, or kept for the next builds. If the VM is
recreated, update the security group target and the EIP's internal IP.

## Step 3: the template

[`example/windows/windows-server-2025.pkr.hcl`](../example/windows/windows-server-2025.pkr.hcl) is the full
template. The options that matter:

```hcl
source "superphenix" "win2025" {
  # api_url, org_id, az, project_id, token, subnet_eid, storage_class ...
  vm_type  = "windows.2k25"  # sets UEFI, Secure Boot and TPM like the console does
  disk_bus = "sata"          # no virtio storage driver during Windows setup
  iso_url  = var.iso_url          # your Windows ISO
  cd_url   = var.answer_iso_url   # the ISO from step 2

  cpu = 4
  memory = "8Gi"
  disk_size = "50Gi"

  boot_wait    = "5s"
  boot_command = ["<spacebar><wait3><spacebar><wait3><spacebar><wait3><spacebar><wait3><enter>"]  # "press any key", then Enter on the Boot Manager menu

  communicator   = "winrm"
  winrm_username = "packer"                  # = WIN_USERNAME of step 1
  winrm_password = var.win_password          # = PASSWORD of step 1 (default Packer-build-1)
  winrm_final_username = var.final_username  # created at the end of the build...
  winrm_final_password = var.final_password  # ...while the build account is deleted at the image's first boot
  winrm_use_ssl  = false
  winrm_insecure = true
  winrm_timeout  = "2h"
}
```

Notes:

- `vm_type` picks the Windows instance type (`windows.2k22`, `windows.2k25`, `windows.11`, ...). Without
  `efi`, `secure_boot` or `tpm`, the type's own firmware defaults apply.
- `cd_url` and `cd_disk_size` (default `1Gi`) are the answer-file ISO. `cd_files` / `cd_content` generate an
  ISO instead, but only work if Superphenix can reach the runner.
- `winrm_final_username` / `winrm_final_password` define the account the image keeps; see [The final account](#the-final-account).
- Pass the token and `final_password` through sensitive variables (`PKR_VAR_token`, `PKR_VAR_final_password`).

Then:

```sh
packer init example/windows
PKR_VAR_iso_url=https://.../windows-server-2025.iso PKR_VAR_answer_iso_url=http://<EIP>/<random>/answers.iso PKR_VAR_final_password=<password> packer build -color=false example/windows
```

## The final account

The answer file creates two accounts: the **build account** (`packer` by default, a local administrator, throwaway
password) that Packer uses over WinRM, and the **built-in administrator** (`Administrator`, `Administrateur`
on a French Windows), which Windows always has and to which the answer file also gives the build password.
Without further settings, both stay in the image with that known password. `winrm_final_username` and
`winrm_final_password` (both, `communicator = "winrm"` only) say who keeps access. After the provisioners the plugin
removes the auto-logon password from the registry and registers a startup task that deletes the build account at
the first boot. The built-in administrator is found by its SID (`...-500`), so the language of the ISO does not
matter. Which case applies depends on the name you choose:

| `winrm_final_username` | What the plugin does with it | The built-in administrator |
|---|---|---|
| The built-in administrator (`Administrator`, `Administrateur`, case-insensitive) | Nothing is created: the account exists. It gets `winrm_final_password` and is enabled (password never expires). | This is the final account. |
| Any other name (for example `ops`) | A new local administrator is created with `winrm_final_password`. | **Disabled, with a random password nobody knows.** It no longer keeps the build password. |
| Empty (with `winrm_final_password` empty) | Nothing: the build account stays. | Stays enabled with the build password. Set the final account, or change the passwords in a provisioner, if the image is shared. |

In every case with a final account, at the first boot of a VM deployed from the image (not during the build, because
Packer's own session runs as the build account):

- the build account `packer` and its profile are deleted, and so is its tile on the logon screen: the only account
  offered is the final one;
- unencrypted Basic authentication on WinRM is turned off;
- if the build account was itself the built-in administrator (`winrm_username = "Administrateur"`), it cannot be deleted:
  it is disabled and gets a random password.

Notes:

- `winrm_final_username` must differ from `winrm_username` (the build account is the one removed).
- The password is uploaded in a script that deletes itself as soon as it runs, never put on a command line, and is
  masked in the logs. Only its hash stays in the image.
- Windows' password policy applies: a password that is too short or too simple makes the build fail at the step
  "Creating account ...".
- Until the first boot of a deployed VM, the build account and the unencrypted Basic authentication still exist in the
  image: do not publish the image without deploying it once.
- Pass the password through a sensitive variable (`PKR_VAR_final_password`).

Validated on Windows Server 2025 (French ISO): with `Administrateur` as the final account (it logs in with the final
password), and with `ops` (it logs in, `Administrateur` is disabled, `packer` is gone, and the logon screen offers
only `ops`).

## What to expect

- The ISO disks import in a few minutes. The Windows installer then takes about **10 minutes just to load**
  (slow `boot.wim` read), before the unattended install starts.
- A full build with Windows Update left out took 36 min 32 s, and 43 min 19 s with the qcow2 export. Set the CI `timeout` accordingly.
- The VM is stopped and kept, ready for the export. The Windows ISO and the answer-file ISO are detached and
  deleted at the end. If the build fails or is cancelled, the VM and all its disks are deleted.

## Troubleshooting

| Symptom | Cause / action |
|---|---|
| Disk import stays `ImportInProgress`, error `Unable to connect to http data source` | The URL is not reachable by Superphenix (a private IP, a closed port, or a security group). Test it from outside the subnet. |
| Stuck at "Waiting for WinRM" | `winrm_username` / `winrm_password` differ from the ISO, the bootstrap did not run (the ISO lacks `windows-initialisation.ps1`; re-generate it with `make-answer-iso.sh`), or the runner cannot reach the VM's private IP on 5985. Watch the VM's console. |
| The build fails at "Creating account ..." | Windows refused the account: `winrm_final_password` does not meet the password policy (length, complexity), or the name is invalid. The message of the script is in the error. |
| Installer says it cannot find an image | `IMAGE_VALUE` does not match an edition of the ISO (see the `dism` command above), or use `IMAGE_KEY=/IMAGE/INDEX`. |
| Installer shows a language or keyboard error | `LOCALE` is not in the ISO, or does not match its language. |
| "No bootable device" or no installer | The firmware is BIOS. Set `vm_type` (or `efi = true`) and keep `disk_bus = "sata"`. |
| Stuck on the **Windows Boot Manager** menu ("Windows Setup [EMS Enabled]") | An extra key press suspended its countdown. End `boot_command` with `<enter>` (as in the example), then it starts Setup. |
| Boot hangs on "Press any key" | Increase the number of `<spacebar>` presses or `boot_wait`. |
| `broken pipe` while typing `boot_command` | Another VNC session took the console (it accepts one at a time). Close other viewers. |

## Limitations

- Validated with Windows Server 2025 (French ISO) only. Windows 10/11 will need their own answer file
  (TPM and Secure Boot checks, edition indexes).
- The export of a Windows VM is validated (qcow2 of 50 GiB virtual size, 11.6 GiB on disk), but booting the exported qcow2 is not tested. It holds an EFI system: boot it with UEFI.
- The machine running Packer needs enough free disk space for the qcow2 (about 13 GB for Server 2025). The plugin checks the free space before the download and stops with a clear message if it is not enough; a local write error also stops it at once.
- Without `winrm_final_username`, the build account and the built-in administrator keep the build password in the image (see [The final account](#the-final-account)). With it, the build account is deleted at the first boot of a VM deployed from the image, not before.
- `floppy_*` is not supported.
