# common/scripts/windows/windows-initialisation.ps1
# Configuration de Windows Remote Management (WinRM) pour permettre
# a Packer de prendre le controle du build via son communicator winrm.

$ErrorActionPreference = 'Continue'

Write-Output '<---------------- Configuration de Windows Remote Management (WINRM) ---------------->'
winrm quickconfig -q
winrm set winrm/config '@{MaxTimeoutms="1800000"}'
winrm set winrm/config/winrs '@{MaxMemoryPerShellMB="800"}'
winrm set winrm/config/service '@{AllowUnencrypted="true"}'
winrm set winrm/config/service/auth '@{Basic="true"}'
winrm set winrm/config/client/auth '@{Basic="true"}'
winrm set winrm/config/listener?Address=*+Transport=HTTP '@{Port="5985"}'

Write-Output '<---------------- Configuration du Pare-Feu ---------------->'
netsh advfirewall firewall set rule group="Windows Remote Administration" new enable=yes
netsh advfirewall firewall set rule group="remote administration" new enable=yes
netsh advfirewall firewall set rule name="Windows Remote Management (HTTP-In)" new enable=yes action=allow

Write-Output '<---------------- Redemarrage des services ---------------->'
net stop winrm
sc.exe config winrm start= auto
net start winrm

# Evite que Windows re-declenche l'auto-logon a l'infini.
# Reference: https://docs.microsoft.com/en-us/windows-hardware/customize/desktop/unattend/microsoft-windows-shell-setup-autologon-logoncount#logoncount-known-issue
Write-Output '<---------------- Reinitialisation du LogonCount ---------------->'
Set-ItemProperty -Path 'HKLM:\SOFTWARE\Microsoft\Windows NT\CurrentVersion\Winlogon' -Name AutoLogonCount -Value 0
