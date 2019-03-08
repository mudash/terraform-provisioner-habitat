package habitat

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"text/template"

	"github.com/hashicorp/terraform/communicator"
	"github.com/hashicorp/terraform/terraform"
	linereader "github.com/mitchellh/go-linereader"
)

const linuxInstallURL = "https://raw.githubusercontent.com/habitat-sh/habitat/master/components/hab/install.sh"
const systemdUnit = `
[Unit]
Description=Habitat Supervisor

[Service]
ExecStart=/bin/hab sup run {{ .SupOptions }}
Restart=on-failure
{{ if .BuilderAuthToken -}}
Environment="HAB_AUTH_TOKEN={{ .BuilderAuthToken }}"
{{ end -}}

[Install]
WantedBy=default.target
`

func (p *provisioner) linuxUploadRingKey(o terraform.UIOutput, comm communicator.Communicator, params ...Params) error {
	command := fmt.Sprintf("echo '%s' | hab ring key import", p.RingKeyContent)
	if p.UseSudo {
		command = fmt.Sprintf("echo '%s' | sudo hab ring key import", p.RingKeyContent)
	}
	return p.runCommand(o, comm, command)
}

func (p *provisioner) linuxInstallHab(o terraform.UIOutput, comm communicator.Communicator, params ...Params) error {
	// Build the install command
	command := fmt.Sprintf("curl -L0 %s > install.sh", linuxInstallURL)
	if err := p.runCommand(o, comm, command); err != nil {
		return err
	}

	// Run the install script
	if p.Version == "" {
		command = fmt.Sprintf("env HAB_NONINTERACTIVE=true bash ./install.sh ")
	} else {
		command = fmt.Sprintf("env HAB_NONINTERACTIVE=true bash ./install.sh -v %s", p.Version)
	}

	if p.UseSudo {
		command = fmt.Sprintf("sudo %s", command)
	}

	if err := p.runCommand(o, comm, command); err != nil {
		return err
	}

	if err := p.createHabUser(o, comm); err != nil {
		return err
	}

	return p.runCommand(o, comm, fmt.Sprintf("rm -f install.sh"))

}

func (p *provisioner) linuxStartHab(o terraform.UIOutput, comm communicator.Communicator, params ...Params) error {
	// Install the supervisor first
	var command string
	if p.Version == "" {
		command += fmt.Sprintf("hab install core/hab-sup")
	} else {
		command += fmt.Sprintf("hab install core/hab-sup/%s", p.Version)
	}

	if p.UseSudo {
		command = fmt.Sprintf("sudo -E %s", command)
	}

	command = fmt.Sprintf("env HAB_NONINTERACTIVE=true %s", command)

	if err := p.runCommand(o, comm, command); err != nil {
		return err
	}

	// Build up sup options
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

	switch p.ServiceType {
	case "unmanaged":
		return p.startHabUnmanaged(o, comm, options)
	case "systemd":
		return p.startHabSystemd(o, comm, options)
	default:
		return errors.New("Unsupported service type")
	}
}

func (p *provisioner) startHabUnmanaged(o terraform.UIOutput, comm communicator.Communicator, options string) error {
	// Create the sup directory for the log file
	var command string
	var token string
	if p.UseSudo {
		command = "sudo mkdir -p /hab/sup/default && sudo chmod o+w /hab/sup/default"
	} else {
		command = "mkdir -p /hab/sup/default && chmod o+w /hab/sup/default"
	}
	if err := p.runCommand(o, comm, command); err != nil {
		return err
	}

	if p.BuilderAuthToken != "" {
		token = fmt.Sprintf("env HAB_AUTH_TOKEN=%s", p.BuilderAuthToken)
	}

	if p.UseSudo {
		command = fmt.Sprintf("(%s setsid sudo -E hab sup run %s > /hab/sup/default/sup.log 2>&1 &) ; sleep 1", token, options)
	} else {
		command = fmt.Sprintf("(%s setsid hab sup run %s > /hab/sup/default/sup.log 2>&1 <&1 &) ; sleep 1", token, options)
	}
	return p.runCommand(o, comm, command)
}

func (p *provisioner) startHabSystemd(o terraform.UIOutput, comm communicator.Communicator, options string) error {
	// Create a new template and parse the client config into it
	unitString := template.Must(template.New("hab-supervisor.service").Parse(systemdUnit))

	var buf bytes.Buffer
	err := unitString.Execute(&buf, p)
	if err != nil {
		return fmt.Errorf("Error executing %s template: %s", "hab-supervisor.service", err)
	}

	var command string
	if p.UseSudo {
		command = fmt.Sprintf("sudo echo '%s' | sudo tee /etc/systemd/system/%s.service > /dev/null", &buf, p.ServiceName)
	} else {
		command = fmt.Sprintf("echo '%s' | tee /etc/systemd/system/%s.service > /dev/null", &buf, p.ServiceName)
	}

	if err := p.runCommand(o, comm, command); err != nil {
		return err
	}

	if p.UseSudo {
		command = fmt.Sprintf("sudo systemctl enable hab-supervisor && sudo systemctl start hab-supervisor")
	} else {
		command = fmt.Sprintf("systemctl enable hab-supervisor && systemctl start hab-supervisor")
	}
	return p.runCommand(o, comm, command)
}

func (p *provisioner) createHabUser(o terraform.UIOutput, comm communicator.Communicator) error {
	addUser := false
	// Install busybox to get us the user tools we need
	command := fmt.Sprintf("env HAB_NONINTERACTIVE=true hab install core/busybox")
	if p.UseSudo {
		command = fmt.Sprintf("sudo %s", command)
	}
	if err := p.runCommand(o, comm, command); err != nil {
		return err
	}

	// Check for existing hab user
	command = fmt.Sprintf("hab pkg exec core/busybox id hab")
	if p.UseSudo {
		command = fmt.Sprintf("sudo %s", command)
	}
	if err := p.runCommand(o, comm, command); err != nil {
		o.Output("No existing hab user detected, creating...")
		addUser = true
	}

	if addUser {
		command = fmt.Sprintf("hab pkg exec core/busybox adduser -D -g \"\" hab")
		if p.UseSudo {
			command = fmt.Sprintf("sudo %s", command)
		}
		return p.runCommand(o, comm, command)
	}

	return nil
}

// In the future we'll remove the dedicated install once the synchronous load feature in hab-sup is
// available. Until then we install here to provide output and a noisy failure mechanism because
// if you install with the pkg load, it occurs asynchronously and fails quietly.
func (p *provisioner) installHabPackage(o terraform.UIOutput, comm communicator.Communicator, service Service) error {
	var command string
	options := ""
	if service.Channel != "" {
		options += fmt.Sprintf(" --channel %s", service.Channel)
	}

	if service.URL != "" {
		options += fmt.Sprintf(" --url %s", service.URL)
	}
	if p.UseSudo {
		command = fmt.Sprintf("env HAB_NONINTERACTIVE=true sudo -E hab pkg install %s %s", service.Name, options)
	} else {
		command = fmt.Sprintf("env HAB_NONINTERACTIVE=true hab pkg install %s %s", service.Name, options)
	}

	if p.BuilderAuthToken != "" {
		command = fmt.Sprintf("env HAB_AUTH_TOKEN=%s %s", p.BuilderAuthToken, command)
	}
	return p.runCommand(o, comm, command)
}

func (p *provisioner) linuxStartHabService(o terraform.UIOutput, comm communicator.Communicator, params ...Params) error {
	var command string
	var service Service
	service = params[0].habService

	if err := p.installHabPackage(o, comm, service); err != nil {
		return err
	}
	if err := p.linuxUploadUserTOML(o, comm, service); err != nil {
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
	if p.UseSudo {
		command = fmt.Sprintf("sudo -E %s", command)
	}
	if p.BuilderAuthToken != "" {
		command = fmt.Sprintf("env HAB_AUTH_TOKEN=%s %s", p.BuilderAuthToken, command)
	}
	return p.runCommand(o, comm, command)
}

func (p *provisioner) uploadServiceGroupKey(o terraform.UIOutput, comm communicator.Communicator, key string) error {
	keyName := strings.Split(key, "\n")[1]
	o.Output("Uploading service group key: " + keyName)
	keyFileName := fmt.Sprintf("%s.box.key", keyName)
	destPath := path.Join("/hab/cache/keys", keyFileName)
	keyContent := strings.NewReader(key)
	if p.UseSudo {
		tempPath := path.Join("/tmp", keyFileName)
		if err := comm.Upload(tempPath, keyContent); err != nil {
			return err
		}
		command := fmt.Sprintf("sudo mv %s %s", tempPath, destPath)
		return p.runCommand(o, comm, command)
	}

	return comm.Upload(destPath, keyContent)
}

func (p *provisioner) linuxUploadUserTOML(o terraform.UIOutput, comm communicator.Communicator, service Service) error {
	// Create the hab svc directory to lay down the user.toml before loading the service
	o.Output("Uploading user.toml for service: " + service.Name)
	destDir := fmt.Sprintf("/hab/svc/%s", service.getPackageName(service.Name))
	command := fmt.Sprintf("mkdir -p %s", destDir)
	if p.UseSudo {
		command = fmt.Sprintf("sudo %s", command)
	}
	if err := p.runCommand(o, comm, command); err != nil {
		return err
	}

	userToml := strings.NewReader(service.UserTOML)

	if p.UseSudo {
		if err := comm.Upload("/tmp/user.toml", userToml); err != nil {
			return err
		}
		command = fmt.Sprintf("sudo mv /tmp/user.toml %s", destDir)
		return p.runCommand(o, comm, command)
	}

	return comm.Upload(path.Join(destDir, "user.toml"), userToml)

}

func (p *provisioner) copyOutput(o terraform.UIOutput, r io.Reader) {
	lr := linereader.New(r)
	for line := range lr.Ch {
		o.Output(line)
	}
}

func getBindFromString(bind string) (Bind, error) {
	t := strings.FieldsFunc(bind, func(d rune) bool {
		switch d {
		case ':', '.':
			return true
		}
		return false
	})
	if len(t) != 3 {
		return Bind{}, errors.New("Invalid bind specification: " + bind)
	}
	return Bind{Alias: t[0], Service: t[1], Group: t[2]}, nil
}
