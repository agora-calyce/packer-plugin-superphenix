# Plugin Packer pour Superphenix — design

## Périmètre
- Plugin Go (`packer-plugin-sdk`) : **builder** + **post-processor**.
- Builder : crée la VM (source ISO ou cloud image `http`), provisionne via communicator (SSH Linux, WinRM Windows).
- Post-processor : exporte le disque en qcow2 vers le runner GitLab (variante 3b).
- Auth : API token (en-tête `Authorization: api-token <token>`, créé dans la console) via variable CI masquée. Client HTTP écrit à la main (pas de SDK Go public).
- Distribution : GitHub Releases, `packer init`.

## API Superphenix utilisée
Base : `/{orgId}/{projectId}/...` (spec : `internal/superphenix-controller/api/docs.go` du repo super-phenix/superphenix).

| Besoin | Endpoint |
|---|---|
| Créer VM / helper | `POST /instance` (`vm.CreateVMInfo`) |
| Stop | `GET /instance/{id}/stop` |
| Détacher disque | `GET /disk/{id}/unmount` |
| Attacher disque | à la création uniquement : `disks[].eid` (pas de mount à chaud) |
| Supprimer | `DELETE /instance/{id}`, `DELETE /disk/{id}` |

## Export qcow2 — variante 3b
1. `stop` de la VM Packer.
2. `unmount` du disque de la VM Packer (le disque est supprimé avec la VM, sinon).
3. Créer la VM helper : cloud image `http` + cloud-init (`qemu-utils`, clé SSH éphémère) + disque Packer en `disks[].eid` (bus secondaire) + EIP + Security Group (SSH depuis l'IP du runner uniquement).
4. SSH helper : `qemu-img convert -O qcow2 <dev> /tmp/out.qcow2`, puis SCP vers le runner (artefact CI).
5. Supprimer le helper (supprime aussi le disque Packer attaché), libérer l'EIP, supprimer le SG.
6. Étape 5 dans un `defer` : exécutée aussi en cas d'échec.

## Hypothèses à valider sur un environnement de test
- `unmount` accepté sur le disque de boot d'une instance arrêtée ; le disque survit à la suppression de la VM.
- Le disque Packer attaché par `eid` au helper est bien supprimé avec le helper.
- Nom de device du disque secondaire dans le helper (`/dev/vdb` ?) — à détecter par taille/serial plutôt que coder en dur.
- Le runner joint l'EIP du helper (egress du runner, IP source stable pour le SG).

## Ouvert
- URL API, endpoint, token : fournis plus tard (envs de test existants).
- Ordre de suppression EIP/SG/helper selon les dépendances Superphenix.
