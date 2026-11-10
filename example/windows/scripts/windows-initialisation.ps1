# Configures Windows Remote Management (WinRM) so that Packer can
# take control of the build through its winrm communicator.

$ErrorActionPreference = 'Continue'

Write-Output '<---------------- Configuring Windows Remote Management (WINRM) ---------------->'
winrm quickconfig -q
winrm set winrm/config '@{MaxTimeoutms="1800000"}'
winrm set winrm/config/winrs '@{MaxMemoryPerShellMB="800"}'
winrm set winrm/config/service '@{AllowUnencrypted="true"}'
winrm set winrm/config/service/auth '@{Basic="true"}'
winrm set winrm/config/client/auth '@{Basic="true"}'
winrm set winrm/config/listener?Address=*+Transport=HTTP '@{Port="5985"}'

Write-Output '<---------------- Configuring the Firewall ---------------->'
netsh advfirewall firewall set rule group="Windows Remote Administration" new enable=yes
netsh advfirewall firewall set rule group="remote administration" new enable=yes
netsh advfirewall firewall set rule name="Windows Remote Management (HTTP-In)" new enable=yes action=allow

Write-Output '<---------------- Restarting services ---------------->'
net stop winrm
sc.exe config winrm start= auto
net start winrm

# Prevents Windows from re-triggering auto-logon forever.
# Reference: https://docs.microsoft.com/en-us/windows-hardware/customize/desktop/unattend/microsoft-windows-shell-setup-autologon-logoncount#logoncount-known-issue
Write-Output '<---------------- Resetting LogonCount ---------------->'
Set-ItemProperty -Path 'HKLM:\SOFTWARE\Microsoft\Windows NT\CurrentVersion\Winlogon' -Name AutoLogonCount -Value 0
