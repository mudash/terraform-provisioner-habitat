package habitat

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/hashicorp/terraform/communicator"
	"github.com/hashicorp/terraform/communicator/remote"
	"github.com/hashicorp/terraform/helper/schema"
	"github.com/hashicorp/terraform/terraform"
)

var serviceTypes = map[string]bool{"unmanaged": true, "systemd": true}
var updateStrategies = map[string]bool{"at-once": true, "rolling": true, "none": true}
var topologies = map[string]bool{"leader": true, "standalone": true}

type provisionFn func(terraform.UIOutput, communicator.Communicator, ...Params) error
type Params struct {
	habService Service
}

type provisioner struct {
	Version          string
	Services         []Service
	PermanentPeer    bool
	ListenGossip     string
	ListenHTTP       string
	Peer             string
	RingKey          string
	RingKeyContent   string
	SkipInstall      bool
	UseSudo          bool
	ServiceType      string
	ServiceName      string
	URL              string
	Channel          string
	Events           string
	OverrideName     string
	Organization     string
	BuilderAuthToken string
	SupOptions       string
	OSType           string

	installHab      provisionFn
	uploadRingKey   provisionFn
	startHab        provisionFn
	startHabService provisionFn
	StartHabService provisionFn
}
type Service struct {
	Name            string
	Strategy        string
	Topology        string
	Channel         string
	Group           string
	URL             string
	Binds           []Bind
	BindStrings     []string
	UserTOML        string
	AppName         string
	Environment     string
	OverrideName    string
	ServiceGroupKey string
}

type Bind struct {
	Alias   string
	Service string
	Group   string
}

func Provisioner() *schema.Provisioner {
	return &schema.Provisioner{
		Schema: map[string]*schema.Schema{
			"version": &schema.Schema{
				Type:     schema.TypeString,
				Optional: true,
			},
			"peer": &schema.Schema{
				Type:     schema.TypeString,
				Optional: true,
			},
			"service_type": &schema.Schema{
				Type:     schema.TypeString,
				Optional: true,
				Default:  "systemd",
			},
			"service_name": &schema.Schema{
				Type:     schema.TypeString,
				Optional: true,
				Default:  "hab-supervisor",
			},
			"use_sudo": &schema.Schema{
				Type:     schema.TypeBool,
				Optional: true,
				Default:  true,
			},
			"permanent_peer": &schema.Schema{
				Type:     schema.TypeBool,
				Optional: true,
				Default:  false,
			},
			"listen_gossip": &schema.Schema{
				Type:     schema.TypeString,
				Optional: true,
			},
			"listen_http": &schema.Schema{
				Type:     schema.TypeString,
				Optional: true,
			},
			"ring_key": &schema.Schema{
				Type:     schema.TypeString,
				Optional: true,
			},
			"ring_key_content": &schema.Schema{
				Type:     schema.TypeString,
				Optional: true,
			},
			"url": &schema.Schema{
				Type:     schema.TypeString,
				Optional: true,
			},
			"channel": &schema.Schema{
				Type:     schema.TypeString,
				Optional: true,
			},
			"events": &schema.Schema{
				Type:     schema.TypeString,
				Optional: true,
			},
			"override_name": &schema.Schema{
				Type:     schema.TypeString,
				Optional: true,
			},
			"organization": &schema.Schema{
				Type:     schema.TypeString,
				Optional: true,
			},
			"builder_auth_token": &schema.Schema{
				Type:     schema.TypeString,
				Optional: true,
			},
			"service": &schema.Schema{
				Type: schema.TypeSet,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"name": &schema.Schema{
							Type:     schema.TypeString,
							Required: true,
						},
						"binds": &schema.Schema{
							Type:     schema.TypeList,
							Elem:     &schema.Schema{Type: schema.TypeString},
							Optional: true,
						},
						"bind": &schema.Schema{
							Type: schema.TypeSet,
							Elem: &schema.Resource{
								Schema: map[string]*schema.Schema{
									"alias": &schema.Schema{
										Type:     schema.TypeString,
										Required: true,
									},
									"service": &schema.Schema{
										Type:     schema.TypeString,
										Required: true,
									},
									"group": &schema.Schema{
										Type:     schema.TypeString,
										Required: true,
									},
								},
							},
							Optional: true,
						},
						"topology": &schema.Schema{
							Type:     schema.TypeString,
							Optional: true,
						},
						"user_toml": &schema.Schema{
							Type:     schema.TypeString,
							Optional: true,
						},
						"strategy": &schema.Schema{
							Type:     schema.TypeString,
							Optional: true,
						},
						"channel": &schema.Schema{

							Type:     schema.TypeString,
							Optional: true,
						},
						"group": &schema.Schema{
							Type:     schema.TypeString,
							Optional: true,
						},
						"url": &schema.Schema{
							Type:     schema.TypeString,
							Optional: true,
						},
						"application": &schema.Schema{
							Type:     schema.TypeString,
							Optional: true,
						},
						"environment": &schema.Schema{
							Type:     schema.TypeString,
							Optional: true,
						},
						"override_name": &schema.Schema{
							Type:     schema.TypeString,
							Optional: true,
						},
						"service_key": &schema.Schema{
							Type:     schema.TypeString,
							Optional: true,
						},
					},
				},
				Optional: true,
			},
		},
		ApplyFunc:    applyFn,
		ValidateFunc: validateFn,
	}
}

func applyFn(ctx context.Context) error {
	o := ctx.Value(schema.ProvOutputKey).(terraform.UIOutput)
	s := ctx.Value(schema.ProvRawStateKey).(*terraform.InstanceState)
	d := ctx.Value(schema.ProvConfigDataKey).(*schema.ResourceData)

	p, err := decodeConfig(d)
	if err != nil {
		return err
	}

	if p.OSType == "" {
		switch t := s.Ephemeral.ConnInfo["type"]; t {
		case "ssh", "": // The default connection type is ssh, so if the type is empty assume ssh
			p.OSType = "linux"
		case "winrm":
			p.OSType = "windows"
		default:
			return fmt.Errorf("Unsupported connection type: %s", t)
		}
	}

	// Set some values based on the targeted OS
	switch p.OSType {
	case "linux":
		p.installHab = p.linuxInstallHab
		p.uploadRingKey = p.linuxUploadRingKey
		p.startHab = p.linuxStartHab
		p.startHabService = p.linuxStartHabService

	case "windows":
		p.installHab = p.winInstallHab
		p.startHabService = p.winStartHabService
		p.startHab = p.winStartHab

	default:
		return fmt.Errorf("Unsupported os type: %s", p.OSType)
	}

	comm, err := communicator.New(s)
	if err != nil {
		return err
	}

	retryCtx, cancel := context.WithTimeout(ctx, comm.Timeout())
	defer cancel()

	err = communicator.Retry(retryCtx, func() error {
		return comm.Connect(o)
	})

	if err != nil {
		return err
	}
	defer comm.Disconnect()

	if !p.SkipInstall {
		o.Output("Installing habitat...")
		if err := p.installHab(o, comm); err != nil {
			o.Output("Error installing habitat...")
			return err
		}
	}

	if p.OSType != "windows" { //ToDo: remove this after adding similar for Win

		if p.RingKeyContent != "" {
			o.Output("Uploading supervisor ring key...")
			if err := p.uploadRingKey(o, comm); err != nil {
				return err
			}
		}
	}
	o.Output("Starting the habitat supervisor...")
	if err := p.startHab(o, comm); err != nil {
		return err
	}
	if p.Services != nil {
		for _, service := range p.Services {
			o.Output("Starting service: " + service.Name)
			if err := p.startHabService(o, comm, Params{habService: service}); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateFn(c *terraform.ResourceConfig) (ws []string, es []error) {
	serviceType, ok := c.Get("service_type")
	if ok {
		if !serviceTypes[serviceType.(string)] {
			es = append(es, errors.New(serviceType.(string)+" is not a valid service_type."))
		}
	}

	builderURL, ok := c.Get("url")
	if ok {
		if _, err := url.ParseRequestURI(builderURL.(string)); err != nil {
			es = append(es, errors.New(builderURL.(string)+" is not a valid URL."))
		}
	}

	// Validate service level configs
	services, ok := c.Get("service")
	if ok {
		for _, service := range services.([]map[string]interface{}) {
			strategy, ok := service["strategy"].(string)
			if ok && !updateStrategies[strategy] {
				es = append(es, errors.New(strategy+" is not a valid update strategy."))
			}

			topology, ok := service["topology"].(string)
			if ok && !topologies[topology] {
				es = append(es, errors.New(topology+" is not a valid topology"))
			}

			builderURL, ok := service["url"].(string)
			if ok {
				if _, err := url.ParseRequestURI(builderURL); err != nil {
					es = append(es, errors.New(builderURL+" is not a valid URL."))
				}
			}
		}
	}
	return ws, es
}

func (p *provisioner) runCommand(o terraform.UIOutput, comm communicator.Communicator, command string) error {
	outR, outW := io.Pipe()
	errR, errW := io.Pipe()

	go p.copyOutput(o, outR)
	go p.copyOutput(o, errR)
	defer outW.Close()
	defer errW.Close()

	cmd := &remote.Cmd{
		Command: command,
		Stdout:  outW,
		Stderr:  errW,
	}

	if err := comm.Start(cmd); err != nil {
		return fmt.Errorf("Error executing command %q: %v", cmd.Command, err)
	}

	if err := cmd.Wait(); err != nil {
		return err
	}

	return nil
}

func decodeConfig(d *schema.ResourceData) (*provisioner, error) {
	p := &provisioner{
		Version:          d.Get("version").(string),
		Peer:             d.Get("peer").(string),
		Services:         getServices(d.Get("service").(*schema.Set).List()),
		UseSudo:          d.Get("use_sudo").(bool),
		ServiceType:      d.Get("service_type").(string),
		ServiceName:      d.Get("service_name").(string),
		RingKey:          d.Get("ring_key").(string),
		RingKeyContent:   d.Get("ring_key_content").(string),
		PermanentPeer:    d.Get("permanent_peer").(bool),
		ListenGossip:     d.Get("listen_gossip").(string),
		ListenHTTP:       d.Get("listen_http").(string),
		URL:              d.Get("url").(string),
		Channel:          d.Get("channel").(string),
		Events:           d.Get("events").(string),
		OverrideName:     d.Get("override_name").(string),
		Organization:     d.Get("organization").(string),
		BuilderAuthToken: d.Get("builder_auth_token").(string),
	}

	return p, nil
}

func getServices(v []interface{}) []Service {
	services := make([]Service, 0, len(v))
	for _, rawServiceData := range v {
		serviceData := rawServiceData.(map[string]interface{})
		name := (serviceData["name"].(string))
		strategy := (serviceData["strategy"].(string))
		topology := (serviceData["topology"].(string))
		channel := (serviceData["channel"].(string))
		group := (serviceData["group"].(string))
		url := (serviceData["url"].(string))
		app := (serviceData["application"].(string))
		env := (serviceData["environment"].(string))
		override := (serviceData["override_name"].(string))
		userToml := (serviceData["user_toml"].(string))
		serviceGroupKey := (serviceData["service_key"].(string))
		var bindStrings []string
		binds := getBinds(serviceData["bind"].(*schema.Set).List())
		for _, b := range serviceData["binds"].([]interface{}) {
			bind, err := getBindFromString(b.(string))
			if err != nil {
				return nil
			}
			binds = append(binds, bind)
		}

		service := Service{
			Name:            name,
			Strategy:        strategy,
			Topology:        topology,
			Channel:         channel,
			Group:           group,
			URL:             url,
			UserTOML:        userToml,
			BindStrings:     bindStrings,
			Binds:           binds,
			AppName:         app,
			Environment:     env,
			OverrideName:    override,
			ServiceGroupKey: serviceGroupKey,
		}
		services = append(services, service)
	}
	return services
}

func getBinds(v []interface{}) []Bind {
	binds := make([]Bind, 0, len(v))
	for _, rawBindData := range v {
		bindData := rawBindData.(map[string]interface{})
		alias := bindData["alias"].(string)
		service := bindData["service"].(string)
		group := bindData["group"].(string)
		bind := Bind{
			Alias:   alias,
			Service: service,
			Group:   group,
		}
		binds = append(binds, bind)
	}
	return binds
}

func (s *Service) getPackageName(fullName string) string {
	return strings.Split(fullName, "/")[1]
}

func (b *Bind) toBindString() string {
	return fmt.Sprintf("%s:%s.%s", b.Alias, b.Service, b.Group)
}
