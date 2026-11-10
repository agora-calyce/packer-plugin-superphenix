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

So the build uses two ISOs and one HTTP server:

```
Windows ISO (iso_url)  ─┐  imported by Superphenix from a public URL
Answer-file ISO (cd_url)┘  static autounattend.xml, imported from a URL you host once
        │ installs Windows, creates the build account, logs in
        ▼
first logon: PowerShell downloads windows-initialisation.ps1 from the Packer HTTP server (the runner,
reached over the private subnet), which enables WinRM
        ▼
Packer connects over WinRM, runs the provisioners, stops the VM
```

Everything that changes between builds (scripts, provisioners) comes from the runner. The answer-file ISO is
static: you rebuild and re-host it only when the image, the language, the build password or the runner address
change.

## Prerequisites

- Packer runs on a VM **inside the subnet** (or behind a VPN), as for any build.
- A security group lets the build VM reach the runner on the Packer HTTP port: **TCP `http_port_min`
  (for example 8080) into the runner, from the subnet CIDR**. Without it the bootstrap fails and Packer waits
  for WinRM until it times out.
- Free disk space on the runner for the exported qcow2 (about 13 GB for Windows Server 2025), if you use the
  export.
- A Windows ISO that Superphenix can download (`iso_url`).
- A place to host the answer-file ISO (step 2).
- `xorriso` and `python3` on the machine that generates the answer-file ISO (`apt install xorriso`).

## Step 1: build the answer-file ISO

`example/windows/answer-iso/make-answer-iso.sh` renders `autounattend.xml` from a template and packs it into a
small ISO (a few hundred KB):

```sh
PASSWORD='<throwaway build password>' \
BOOTSTRAP_URL=http://RUNNER_IP:8080 \
LOCALE=fr-FR \
IMAGE_VALUE='Windows Server 2025 SERVERSTANDARD' \
sh example/windows/answer-iso/make-answer-iso.sh answers-win2025-fr.iso
```

| Variable | Default | Meaning |
|---|---|---|
| `PASSWORD` | required | Password of the build account (also the local Administrator). Use the **same value** as `winrm_password` in the template. |
| `BOOTSTRAP_URL` | required | Packer HTTP server as the VM sees it: `http://<runner private IP>:<http_port_min>`. |
| `USERNAME` | `Administrateur` | Build account. Must match `winrm_username`. Use `Administrator` on an English ISO. |
| `LOCALE` | `fr-FR` | Language and keyboard of the installer. Must be one the Windows ISO contains. |
| `TIMEZONE` | `Romance Standard Time` | Windows time zone name. |
| `HOSTNAME` | `windows-build` | Computer name. |
| `IMAGE_KEY` / `IMAGE_VALUE` | `/IMAGE/NAME`, `Windows Server 2025 SERVERSTANDARD` | Edition to install. Use `IMAGE_KEY=/IMAGE/INDEX` with `IMAGE_VALUE=2` to select by index. |
| `PRODUCT_KEY` | empty | Product key, if the ISO needs one (an evaluation ISO does not). |

To list the editions of an ISO, mount it and run `dism /Get-WimInfo /WimFile:D:\sources\install.wim` on
Windows, or `wiminfo install.wim` from `wimtools` on Linux.

The ISO contains the build password in clear text: treat it as a secret, host it at an unguessable URL and
remove it when you are done. The password stays valid in the final image unless a provisioner changes it.

You need one ISO per combination of image, language, password and runner address. A different Windows version
usually only changes `IMAGE_VALUE` (or the index).

### Validate the answer file

`validate-answer-iso.py` checks an `autounattend.xml`, or an ISO that contains one, before you host it. It needs
only Python 3 (and `xorriso` to read an ISO):

```sh
WINRM_PASSWORD='<throwaway build password>' \
  python3 example/windows/answer-iso/validate-answer-iso.py answers-win2025-fr.iso \
  --username Administrateur --bootstrap-url http://RUNNER_IP:8080
```

It fails (exit status 1) when:

- `autounattend.xml` is not at the root of the ISO, or the XML is not well-formed, or placeholders such as
  `@PASSWORD@` were left unreplaced;
- the EULA is not accepted, no image to install is set, or the disk has no EFI partition (the VM boots with UEFI);
- there is no build account, no auto-logon, or no `FirstLogonCommands` (nothing would enable WinRM);
- `--username` / `WINRM_PASSWORD` do not match the account in the file (they must equal `winrm_username` /
  `winrm_password` in your template).

It warns about clear-text passwords, a `FirstLogonCommands` that never mentions WinRM, and (with
`--bootstrap-url`, run from the runner while Packer's HTTP server is up) an unreachable bootstrap script.
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
   firewall. Either download the bootstrap from the Packer HTTP server as the template does (the build VM
   must reach `http_port_min` on the runner), or put the script on your ISO and run it from the CD at first
   logon, in which case Packer's HTTP server is not needed. `example/windows/scripts/windows-initialisation.ps1`
   is a working script.
   This setting must not reach the image: `example/windows/scripts/windows-cleanup.ps1` registers a one-shot
   startup task that turns unencrypted Basic authentication off at the first boot of a deployed VM (doing it
   during the build would cut Packer's own session). Keep it as the last provisioner.
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
  iso_url  = "https://.../Win_Server_STD_CORE_2025_French.ISO"
  cd_url   = var.answer_iso_url   # the ISO from step 2

  cpu = 4
  memory = "8Gi"
  disk_size = "50Gi"

  http_directory = "${path.root}/scripts"  # serves windows-initialisation.ps1 to the VM
  http_port_min  = 8080
  http_port_max  = 8080

  boot_wait    = "5s"
  boot_command = ["<spacebar><wait3><spacebar><wait3><spacebar><wait3><spacebar><wait3><enter>"]  # "press any key", then Enter on the Boot Manager menu

  communicator   = "winrm"
  winrm_username = "Administrateur"
  winrm_password = var.win_password          # = PASSWORD of step 1
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
- The first `FirstLogonCommand` retries the bootstrap download for about 10 minutes, so the runner must be
  reachable on `http_port_min` from the VM before then.
- Pass the token and `win_password` through sensitive variables (`PKR_VAR_token`, `PKR_VAR_win_password`).

Then:

```sh
packer init example/windows
PKR_VAR_answer_iso_url=http://<EIP>/<random>/answers.iso packer build -color=false example/windows
```

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
| Stuck at "Waiting for WinRM" | The VM cannot reach the runner on `http_port_min` (security group), `BOOTSTRAP_URL` is wrong, or `winrm_username` / `winrm_password` differ from the ISO. Watch the VM's console. |
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
- The build password remains in the image; change it in a provisioner if the image is shared.
- `floppy_*` is not supported.
