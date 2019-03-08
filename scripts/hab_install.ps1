[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
iwr https://api.bintray.com/content/habitat/stable/windows/x86_64/hab-%24latest-x86_64-windows.zip?bt_package=hab-x86_64-windows -Outfile c:\habitat.zip
Expand-Archive c:/habitat.zip c:/
mv c:/hab-* c:/habitat
$env:Path = $env:Path,"C:\habitat" -join ";"
[System.Environment]::SetEnvironmentVariable('Path', $env:Path, [System.EnvironmentVariableTarget]::Machine)
# Install hab as a Windows service
hab pkg install core/windows-service
hab pkg exec core/windows-service install
# Add config to HabService.dll.config
$svcPath = Join-Path $env:SystemDrive "hab\svc\windows-service"
[xml]$configXml = Get-Content (Join-Path $svcPath HabService.dll.config)
$configXml.configuration.appSettings.add[2].value = "--no-color --peer ${peer}"
$configXml.Save((Join-Path $svcPath HabService.dll.config))
# Start service
Start-Service Habitat
New-NetFirewallRule -DisplayName "Habitat TCP" -Direction Inbound -Action Allow -Protocol TCP -LocalPort 9631,9638
New-NetFirewallRule -DisplayName "Habitat UDP" -Direction Inbound -Action Allow -Protocol UDP -LocalPort 9638