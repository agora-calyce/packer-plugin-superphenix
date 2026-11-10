# Windows Server 2025 example

Full guide: [`docs/windows.md`](../../docs/windows.md).

| File | Role |
|---|---|
| `windows-server-2025.pkr.hcl` | Template: ISO install, WinRM, provisioners. Variable `answer_iso_url` is the hosted answer-file ISO. |
| `answer-iso/make-answer-iso.sh` | Builds the answer-file ISO (`autounattend.xml` rendered from `autounattend.xml.in`). Host it, then pass its URL as `answer_iso_url`. |
| `answer-iso/autounattend.example.xml` | A rendered, validated sample (dummy password `ChangeMe-Example1`, bootstrap `http://RUNNER_IP:8080`): the reference for writing your own `autounattend.xml`. Regenerate it with `make-answer-iso.sh`'s renderer if the template changes. |
| `answer-iso/validate-answer-iso.py` | Checks an `autounattend.xml` or an answer-file ISO (UEFI layout, build account, WinRM, credentials) before you host it. |
| `scripts/windows-initialisation.ps1` | Bootstrap served by Packer's HTTP server and run at first logon: enables WinRM and opens the firewall. |
| `scripts/windows-preparation.ps1`, `windows-cleanup.ps1` | Hardening and cleanup provisioners. The cleanup, run last, also registers the startup task that turns the build's unencrypted Basic WinRM off at the first boot of a deployed VM. |

Quick start: generate the ISO (`PASSWORD=... BOOTSTRAP_URL=http://<runner ip>:8080 sh answer-iso/make-answer-iso.sh out.iso`),
host it at an unguessable URL reachable by Superphenix, allow TCP 8080 from the subnet into the runner, then
`PKR_VAR_answer_iso_url=<url> packer build -color=false .` with `PKR_VAR_win_password` set to the same password.
