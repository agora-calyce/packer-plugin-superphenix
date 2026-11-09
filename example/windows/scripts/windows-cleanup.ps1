# common/scripts/windows/windows-cleanup.ps1
# Nettoyage des fichiers temporaires et de Windows Update avant export.

$servicesToRecycle = "wuauserv", "cryptSvc", "bits", "msiserver"
$tempFoldersToClear = @(
    "C:\Windows\Temp",
    "C:\Users\*\AppData\Local\Temp",
    "C:\Windows\SoftwareDistribution\Download",
    "C:\Windows\SoftwareDistribution\DataStore\Logs",
    "C:\Windows\Panther"
)

Stop-Service -Name $servicesToRecycle -Force -ErrorAction SilentlyContinue

# -Exclude packer-* : Packer depose ses propres fichiers d'orchestration
# (env-vars, wrapper) dans C:\Windows\Temp pendant l'execution elevated en
# cours ; les supprimer en aveugle casse l'etape courante.
foreach ($folder in $tempFoldersToClear) {
    Get-ChildItem $folder -Recurse -Force -ErrorAction SilentlyContinue |
        Where-Object { $_.Name -notlike 'packer-*' } |
        Remove-Item -Force -Recurse -ErrorAction SilentlyContinue
}

# Fichier genere par le provisioner de liste de paquets (deja telecharge
# sur l'hote a ce stade, plus besoin de le garder dans l'image).
Remove-Item -Path "C:\packages.csv" -Force -ErrorAction SilentlyContinue

Start-Service -Name $servicesToRecycle -ErrorAction SilentlyContinue

# autounattend.xml desactive le pare-feu pendant l'install (EnableFirewall
# false) pour simplifier le build. Rien ne le reactivait ensuite -> l'image
# finale partait avec le pare-feu desactive en permanence.
Set-NetFirewallProfile -All -Enabled True
