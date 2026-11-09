# Plugin Packer pour Superphenix — design

## Périmètre
- Plugin Go (`packer-plugin-sdk`) : **builder** + **post-processor**.
- Builder : crée la VM depuis une cloud image (`source_url`) ou installe depuis une ISO (`iso_url`), provisionne via SSH (Linux) ou WinRM (Windows). Windows : ISO d'answer file hébergée (`cd_url`), validé avec Server 2025 FR ; `cd_files`/`cd_content` seulement si Superphenix joint le runner ; pas de `floppy`.
- Post-processor : exporte le disque en qcow2 vers la machine qui lance Packer (variante 3b).
- Auth : API token (en-tête `Authorization: api-token <token>`, créé dans la console) via variable CI masquée. Client HTTP écrit à la main (pas de SDK Go public).
- Réseau : le runner joint les **IP privées** des VM (builder et helper d'export). Il doit donc être dans le subnet Superphenix, ou derrière un VPN. L'exposition par EIP n'est pas supportée.
- Distribution : GitHub Releases, `packer init` (token `PACKER_GITHUB_API_TOKEN` si le dépôt est privé).

## API Superphenix utilisée
Base : `{api_url}/{orgId}/api/spx-ctrl/{az}/{projectId}/...`. Spec : `https://docs.superphenix.net/assets/openapi/superphenix-api.json` (source : `internal/superphenix-api/api/docs.go` dans super-phenix/superphenix).

| Besoin | Endpoint |
|---|---|
| Créer un disque | `POST /disk` (`storage` = entier en Gio, `storageClass` obligatoire, `source.type` = `http`) |
| Attendre l'import | `GET /disk/{id}` → `disk.status.phase == "Succeeded"` |
| Créer VM / helper | `POST /instance` (`network` obligatoire, `compute.memory` entier en Gio, labels `user.superphenix.net/<clé>:<valeur>`) |
| IP / état | `GET /instance/{id}` (`vmi.status.interfaces[].ipAddresses`, `vm.status.printableStatus`) |
| Stop | `GET /instance/{id}/stop` |
| Détacher un disque | `GET /disk/{id}/unmount` |
| Attacher un disque | à la création de la VM seulement : `disks[].eid` |
| Supprimer | `DELETE /instance/{id}`, `DELETE /disk/{id}` (réessayé tant que le disque est « mounted ») |

Points à connaître :
- `cloudInit.custom = true` **remplace** la cloud-init de Superphenix (qui crée l'utilisateur `spx`) : le plugin crée lui-même l'utilisateur SSH avec `sudo` sans mot de passe.
- Supprimer une VM ne supprime pas ses disques : le plugin les crée à part et les supprime explicitement.
- La suppression d'une VM est asynchrone : son disque reste « mounted » un moment.
- Une VM qui démarre avant la fin de l'import de son disque de boot peut démarrer sur un autre disque : on attend `Succeeded`.

## Installation depuis une ISO
Testé avec Ubuntu Server 26.04 (2026-11-08).
- Disques : un disque vide (`source.type = "blank"`, minuscules) en `order 0` (virtio) et l'ISO (`http`, `cdrom: true`, bus `sata`) en `order 1`. Attendre `Succeeded` sur les deux avant de créer la VM.
- **EFI obligatoire** (`advanced.firmware.bootloader.efi.enabled`) : en BIOS (SeaBIOS) seul le disque dur est essayé, le CD-ROM n'est jamais amorcé. Avec EFI, le disque vide n'est pas amorçable donc l'ISO démarre, puis le disque installé démarre seul au reboot de l'installeur : pas besoin d'éjecter le CD-ROM. Pas de `bootOrder` dans l'API.
- `boot_command` : `GET .../instance/{id}/vnc` est un WebSocket (en-tête `Authorization`) qui parle RFB 003.008 (sécurité « None »). Le plugin envoie seulement des `KeyEvent` (`internal/client/vnc.go`) via le pilote VNC du SDK. Une capture d'écran se lit avec les rectangles Raw (outil jetable, non conservé).
- Fichier de réponse : `cloudInit.config` de l'instance (disque `cidata` lu par l'installeur). Évite le serveur HTTP de Packer, que la VM ne joint pas si un groupe de sécurité bloque le runner. `http_content` reste possible.
- Ubuntu 26.04, GRUB EFI : la ligne `linux /casper/vmlinuz  ---` est la 4e ligne de l'éditeur ; `autoinstall` doit être inséré **avant** `---` (`<end><left><left><left><left>` : il y a un caractère après les tirets). À vérifier par capture à chaque nouvelle release.
- Le disque ISO est détaché (`unmount`) puis supprimé à la fin du build. Il reste attaché pendant le provisioning.
- Export validé depuis une ISO Ubuntu 26.04 (2026-11-09, 22 min 41 s, qcow2 de 6 Go) et Windows. Un 1er essai le même jour a fini en « Timeout waiting for SSH » (40 min) sans cause trouvée ; le rejeu identique a réussi (non reproduit, probable sensibilité au timing du boot_command). Non testé : le démarrage d'un qcow2 exporté, Secure Boot et TPM sur Linux, `cd_files` sur un runner joignable par Superphenix.

## Export qcow2 — variante 3b
1. `stop` de la VM Packer (le builder), puis attente de l'état `Stopped`.
2. `unmount` du disque Packer, puis suppression de la VM Packer (le disque survit).
3. Créer le disque de boot du helper (cloud image `http`) et attendre son import.
4. Créer la VM helper : cloud-init (`qemu-utils`, clé SSH éphémère), disque de boot + disque Packer en second volume (`disks[].eid`).
5. SSH vers l'IP privée du helper : détecter le disque exporté (ni la racine du helper, ni le disque cloud-init, sauf `disk_device`), `qemu-img convert -O qcow2`, puis téléchargement via stdout SSH, avec reprise si le flux se fige 60 s.
6. Supprimer helper puis disques. Tout est dans des `defer` : exécuté aussi en cas d'échec ou d'annulation (Ctrl-C ferme la connexion SSH et supprime le fichier partiel).

## Limites connues
- Testé avec une version de l'API et une AZ (`fr01-als01`) ; la version déployée attend les tailles (`memory`, `storage`) en entier de Gio sans unité. Le plugin accepte `4Gi` ou `4` et envoie `4` ; le code récent de `main` de l'API attend une quantité (`"4Gi"`) : à suivre à la mise à jour de l'API.
- Clé hôte SSH du helper non vérifiée (VM éphémère, clé inconnue d'avance).
- La suppression d'un disque peut renvoyer 500 jusqu'à plusieurs minutes après la suppression de sa VM : le plugin réessaie 10 min.
- Si `Ctrl-C` ou un crash tue Packer pendant le build, les ressources déjà créées peuvent rester : les labels `user.superphenix.net/created-by:packer*` permettent de les repérer.

## Pistes
- Runner hors réseau : exposer la VM par EIP + DNAT + SG, pour le builder comme pour le helper. Demande de lire et de restaurer l'état de l'EIP (SNAT, DNAT existants) ; non fait.
- Release : fichiers préparés mais inactifs dans `release/` (goreleaser, workflows de release et de CI, procédure d'activation dans `release/README.md`) ; à activer une fois les tests élargis. Non testés (ni goreleaser ni GitHub Actions disponibles ici).

## Windows

- L'import d'un disque `http` est fait par un pod de Superphenix qui n'a **aucune route vers l'IP privée du runner** (timeout, aucun SYN côté runner). Une ISO générée par le runner (`cd_files`/`cd_content`) est donc inimportable ; l'ISO d'answer file est statique et hébergée (`cd_url`, `example/windows/answer-iso/make-answer-iso.sh`).
- Les VMs joignent le runner (groupe de sécurité 10.0.0.0/24 vers `http_port`). Le bootstrap WinRM est téléchargé au premier logon par PowerShell depuis `http_directory`.
- WinPE de l'ISO Server 2025 FR : Shift+F10 ouvre `X:\sources>` ; ni curl, ni certutil, ni PowerShell ; clavier AZERTY (le VNC envoie des keycodes US) ; ~10 min de chargement (`<wait600s>`).
- `vm_type = "windows.2k25"` fixe EFI, Secure Boot et TPM : sans `efi`/`secure_boot`/`tpm` explicites, le builder n'envoie pas `advanced`. Disque système en SATA (`disk_bus`), pas de pilote virtio à l'installation.
- Console VNC : une seule session à la fois.
- Validé de bout en bout : Server 2025 FR, 36 min 32 s (ISO + `cd_url` d'answer file hébergée sur `iso-host`, WinRM via bootstrap PowerShell, hardening, nettoyage). Export qcow2 d'une VM Windows validé le 2026-11-09 : 43 min 19 s au total, fichier de 12,46 Go (50 GiB virtuels, 11,6 GiB utilisés). Le premier essai avait échoué car `/home` du runner n'avait que 915 Mo libres : le plugin prenait l'erreur d'écriture locale pour un blocage réseau (reprises « stalled » puis `gave up after 20 resumes`). Corrigé : `localWriter` (`transfer.go`) arrête tout de suite sur une erreur d'écriture locale, et `freeBytes` compare l'espace libre à la taille du qcow2 avant le téléchargement (non implémenté sous Windows : contrôle ignoré). Le contrôle se fait après la conversion : la taille du qcow2 n'est connue qu'à ce moment.
- L'answer file est statique (`example/windows/answer-iso/`, générateur et validateur) ; tout ce qui varie d'un build vient du serveur HTTP du runner. Les IDs de l'infrastructure de test (`iso-host`, EIP, groupes de sécurité) sont dans `docs/test-infrastructure.md`.
- Guides utilisateur : `docs/windows.md` et `docs/linux.md`.
