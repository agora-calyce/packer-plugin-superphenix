# Plugin Packer pour Superphenix — design

## Périmètre
- Plugin Go (`packer-plugin-sdk`) : **builder** + **post-processor**.
- Builder : crée la VM depuis une cloud image (source `http`), provisionne via le communicator SSH. Pas d'ISO, pas de WinRM.
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

## Export qcow2 — variante 3b
1. `stop` de la VM Packer (le builder), puis attente de l'état `Stopped`.
2. `unmount` du disque Packer, puis suppression de la VM Packer (le disque survit).
3. Créer le disque de boot du helper (cloud image `http`) et attendre son import.
4. Créer la VM helper : cloud-init (`qemu-utils`, clé SSH éphémère), disque de boot + disque Packer en second volume (`disks[].eid`).
5. SSH vers l'IP privée du helper : détecter le disque exporté (ni la racine du helper, ni le disque cloud-init, sauf `disk_device`), `qemu-img convert -O qcow2`, puis téléchargement via stdout SSH, avec reprise si le flux se fige 60 s.
6. Supprimer helper puis disques. Tout est dans des `defer` : exécuté aussi en cas d'échec ou d'annulation (Ctrl-C ferme la connexion SSH et supprime le fichier partiel).

## Limites connues
- Testé avec une version de l'API et une AZ (`fr01-als01`) ; la version déployée attend `memory` en entier de Gio, alors que le code récent de `main` attend une quantité (`"4Gi"`) : à suivre à la mise à jour de l'API.
- Clé hôte SSH du helper non vérifiée (VM éphémère, clé inconnue d'avance).
- Si `Ctrl-C` ou un crash tue Packer pendant le build, les ressources déjà créées peuvent rester : les labels `user.superphenix.net/created-by:packer*` permettent de les repérer.

## Pistes
- Runner hors réseau : exposer la VM par EIP + DNAT + SG, pour le builder comme pour le helper. Demande de lire et de restaurer l'état de l'EIP (SNAT, DNAT existants) ; non fait.
- Release : goreleaser (binaires `linux/amd64`, `linux/arm64`, checksums) une fois les tests élargis.
