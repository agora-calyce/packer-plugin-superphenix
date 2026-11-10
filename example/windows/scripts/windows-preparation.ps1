# Basic hardening: disables TLS 1.0/1.1, SMBv1 and PowerShell v2.

$ErrorActionPreference = 'Stop'

Write-Output '<---------------- Disabling the "Windows Admin Center" pop-up ---------------->'
New-ItemProperty -Path "HKLM:\SOFTWARE\Microsoft\ServerManager" -Name "DoNotPopWACConsoleAtSMLaunch" -Value 1 -PropertyType DWord -Force | Out-Null

Write-Output '<---------------- Disabling TLS 1.0 and 1.1 ---------------->'
foreach ($tlsVersion in @('TLS 1.0', 'TLS 1.1')) {
    $basePath = "HKLM:\SYSTEM\CurrentControlSet\Control\SecurityProviders\SCHANNEL\Protocols\$tlsVersion"
    foreach ($role in @('Server', 'Client')) {
        New-Item -Path "$basePath\$role" -Force | Out-Null
        New-ItemProperty -Path "$basePath\$role" -Name "Enabled" -Value 0 -PropertyType DWord -Force | Out-Null
        New-ItemProperty -Path "$basePath\$role" -Name "DisabledByDefault" -Value 1 -PropertyType DWord -Force | Out-Null
    }
}

Write-Output '<---------------- Removing PowerShell v2 and SMBv1 ---------------->'
# Both features do not necessarily exist depending on the Windows version
# (e.g. MicrosoftWindowsPowerShellV2 is absent on Server 2025) -> only
# disable a feature if it exists and is enabled.
foreach ($feature in @('MicrosoftWindowsPowerShellV2', 'SMB1Protocol')) {
    if ((Get-WindowsOptionalFeature -Online -FeatureName $feature -ErrorAction SilentlyContinue).State -eq 'Enabled') {
        Disable-WindowsOptionalFeature -Online -FeatureName $feature -NoRestart | Out-Null
    }
}
