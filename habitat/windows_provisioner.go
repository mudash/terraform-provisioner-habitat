package habitat

import (
	"fmt"
	"path"
	"strings"

	"github.com/hashicorp/terraform/communicator"
	"github.com/hashicorp/terraform/terraform"
)

const installScript = `
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
iwr https://api.bintray.com/content/habitat/stable/windows/x86_64/hab-%%24latest-x86_64-windows.zip?bt_package=hab-x86_64-windows -Outfile c:\habitat.zip
Expand-Archive c:/habitat.zip c:/
mv c:/hab-* c:/habitat
$env:Path = $env:Path,"C:\habitat" -join ";"
[System.Environment]::SetEnvironmentVariable('Path', $env:Path, [System.EnvironmentVariableTarget]::Machine)
# Install hab as a Windows service
hab pkg install core/windows-service
hab pkg exec core/windows-service install
New-NetFirewallRule -DisplayName "Habitat TCP" -Direction Inbound -Action Allow -Protocol TCP -LocalPort 9631,9638
New-NetFirewallRule -DisplayName "Habitat UDP" -Direction Inbound -Action Allow -Protocol UDP -LocalPort 9638
`

func (p *provisioner) winInstallHab(o terraform.UIOutput, comm communicator.Communicator, param ...Params) error {

	script := path.Join(path.Dir(comm.ScriptPath()), "win_hab_install.ps1")
	content := fmt.Sprintf(installScript)

	// Upload the script to target instance
	if err := comm.UploadScript(script, strings.NewReader(content)); err != nil {
		return fmt.Errorf("Uploading win_hab_install.ps1 failed: %v", err)
	}
	// Execute Powershell script
	installCmd := fmt.Sprintf("powershell -NoProfile -ExecutionPolicy Bypass -File %s", script)
	return p.runCommand(o, comm, installCmd)
}

func (p *provisioner) winStartHab(o terraform.UIOutput, comm communicator.Communicator, params ...Params) error {

	var content string
	options := ""
	if p.PermanentPeer {
		options += " -I"
	}

	if p.ListenGossip != "" {
		options += fmt.Sprintf(" --listen-gossip %s", p.ListenGossip)
	}

	if p.ListenHTTP != "" {
		options += fmt.Sprintf(" --listen-http %s", p.ListenHTTP)
	}

	if p.Peer != "" {
		options += fmt.Sprintf(" --peer %s", p.Peer)
	}

	if p.RingKey != "" {
		options += fmt.Sprintf(" --ring %s", p.RingKey)
	}

	if p.URL != "" {
		options += fmt.Sprintf(" --url %s", p.URL)
	}

	if p.Channel != "" {
		options += fmt.Sprintf(" --channel %s", p.Channel)
	}

	if p.Events != "" {
		options += fmt.Sprintf(" --events %s", p.Events)
	}

	if p.OverrideName != "" {
		options += fmt.Sprintf(" --override-name %s", p.OverrideName)
	}

	if p.Organization != "" {
		options += fmt.Sprintf(" --org %s", p.Organization)
	}

	p.SupOptions = options
	content += fmt.Sprintf("$svcPath = Join-Path $env:SystemDrive \"hab\\svc\\windows-service\"\n")
	content += fmt.Sprintf("[xml]$configXml = Get-Content (Join-Path $svcPath HabService.dll.config)\n")
	content += fmt.Sprintf("$configXml.configuration.appSettings.add[2].value = \"%s\"\n", options)
	content += fmt.Sprintf("$configXml.Save((Join-Path $svcPath HabService.dll.config))\n")
	content += fmt.Sprintf("Start-Service Habitat\n")

	script := path.Join(path.Dir(comm.ScriptPath()), "win_hab_start.ps1")

	// Upload the script to target instance
	if err := comm.UploadScript(script, strings.NewReader(content)); err != nil {
		return fmt.Errorf("Uploading win_hab_start.ps1 failed: %v", err)
	}
	// Execute Powershell script
	installCmd := fmt.Sprintf("powershell -NoProfile -ExecutionPolicy Bypass -File %s", script)
	return p.runCommand(o, comm, installCmd)

}

func (p *provisioner) winStartHabService(o terraform.UIOutput, comm communicator.Communicator, params ...Params) error {

	var command string
	var service Service
	service = params[0].habService

	if err := p.winUploadUserTOML(o, comm, service); err != nil {
		return err
	}

	// Upload service group key
	if service.ServiceGroupKey != "" {
		p.uploadServiceGroupKey(o, comm, service.ServiceGroupKey)
	}

	options := ""
	if service.Topology != "" {
		options += fmt.Sprintf(" --topology %s", service.Topology)
	}

	if service.Strategy != "" {
		options += fmt.Sprintf(" --strategy %s", service.Strategy)
	}

	if service.Channel != "" {
		options += fmt.Sprintf(" --channel %s", service.Channel)
	}

	if service.URL != "" {
		options += fmt.Sprintf(" --url %s", service.URL)
	}

	if service.Group != "" {
		options += fmt.Sprintf(" --group %s", service.Group)
	}

	for _, bind := range service.Binds {
		options += fmt.Sprintf(" --bind %s", bind.toBindString())
	}
	command = fmt.Sprintf("hab svc load %s %s", service.Name, options)

	if p.BuilderAuthToken != "" {
		command = fmt.Sprintf("set HAB_AUTH_TOKEN=%s %s", p.BuilderAuthToken, command)
	}
	return p.runCommand(o, comm, command)
}

func (p *provisioner) winUploadUserTOML(o terraform.UIOutput, comm communicator.Communicator, service Service) error {
	// Create the hab svc directory to lay down the user.toml before loading the service
	o.Output("Uploading user.toml for service: " + service.Name)
	svcName := service.getPackageName(service.Name)
	destDir := fmt.Sprintf("C:\\hab\\user\\%s\\config", svcName)
	command := fmt.Sprintf("mkdir %s", destDir)

	if err := p.runCommand(o, comm, command); err != nil {
		return err
	}

	userToml := strings.NewReader(service.UserTOML)

	/*if err := comm.Upload(fmt.Sprintf("C:\\temp\\%s-user.toml", svcName), userToml); err != nil {
		return err
	}

	command = fmt.Sprintf("move C:\\temp\\%s-user.toml %s\\user.toml", svcName, destDir)
	o.Output(command)
	return p.runCommand(o, comm, command)
	*/
	return comm.Upload(path.Join(destDir, "user.toml"), userToml)

}
