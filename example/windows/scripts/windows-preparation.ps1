# common/scripts/windows/windows-preparation.ps1
# Durcissement de base : desactive TLS 1.0/1.1 et SMBv1, nettoie les logs
# d'evenements, supprime PowerShell v2.

$ErrorActionPreference = 'Stop'

Write-Output '<---------------- Desactivation de la pop-up "Windows Admin Center" ---------------->'
New-ItemProperty -Path "HKLM:\SOFTWARE\Microsoft\ServerManager" -Name "DoNotPopWACConsoleAtSMLaunch" -Value 1 -PropertyType DWord -Force | Out-Null

Write-Output '<---------------- Desactivation de TLS 1.0 et 1.1 ---------------->'
foreach ($tlsVersion in @('TLS 1.0', 'TLS 1.1')) {
    $basePath = "HKLM:\SYSTEM\CurrentControlSet\Control\SecurityProviders\SCHANNEL\Protocols\$tlsVersion"
    foreach ($role in @('Server', 'Client')) {
        New-Item -Path "$basePath\$role" -Force | Out-Null
        New-ItemProperty -Path "$basePath\$role" -Name "Enabled" -Value 0 -PropertyType DWord -Force | Out-Null
        New-ItemProperty -Path "$basePath\$role" -Name "DisabledByDefault" -Value 1 -PropertyType DWord -Force | Out-Null
    }
}

Write-Output '<---------------- Suppression de PowerShell v2 et SMBv1 ---------------->'
# Les deux features n'existent plus forcement selon la version de Windows
# (ex: MicrosoftWindowsPowerShellV2 absente sur Server 2025) -> on ne
# desactive que si la feature existe et est activee.
foreach ($feature in @('MicrosoftWindowsPowerShellV2', 'SMB1Protocol')) {
    if ((Get-WindowsOptionalFeature -Online -FeatureName $feature -ErrorAction SilentlyContinue).State -eq 'Enabled') {
        Disable-WindowsOptionalFeature -Online -FeatureName $feature -NoRestart | Out-Null
    }
}
