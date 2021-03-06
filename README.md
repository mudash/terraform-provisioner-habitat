### The Habitat provisioner has been moved into core Terraform (https://github.com/hashicorp/terraform/tree/master/builtin/provisioners/habitat)

## This is a work in progress, development repository to add more features into Habitat terraform provisioner

# terraform-provisioner-habitat
A [Habitat](https://habitat.sh) provisioner for [Terraform](https://terraform.io)

## **NOTE:  This is in a super early state.**
* To this point, I have only tested using AWS.
* No validation of the provisioner data is being done yet.
* The only Operating Sytems that have been tested are Amazon Linux 2017.03.0, and Ubuntu 14.04 and 16.04
* Go tests for the provisioner itself are still to come

That being said, please try it out and report any issues you come across!

## Installation
### Download binaries
Builds for macOS, Linux, and Windows are attached to GitHub releases.

### Build from source
```bash
git clone git@github.com:nsdavidson/terraform-provisioner-habitat.git
cd terraform-provisioner-habitat
go build
```

After getting or building a copy of the provisioner plugin, add the following to your ~/.terraformrc (create the file if it doesn't exist)
```
provisioners {
  habitat = "<path to the plugin binary>"
}
```

## Requirements
* This provisioner will currently only work on Linux targets.  As the Habitat supervisor becomes available on more systems, support for those will be added.
* Currently, we assume several userspace utilities on the target system (curl, wget, setsid, tee, etc).  
* You must have SSH access as root or a user that can passwordless sudo.

## Usage
Example to spin up a 3 node redis cluster:
```hcl
resource "aws_instance" "redis" {
  ami = "ami-12345"
  instance_type = "t2.micro"
  key_name = "foo"
  count = 3

  provisioner "habitat" {
    peer = "${aws_instance.redis.0.private_ip}"
    use_sudo = true
    
    service {
      name = "core/redis"
      topology = "leader"
      user_toml = "${file("conf/redis.toml")}"
    }
  }
}
```

Example with service binding:
```hcl
resource "aws_instance" "web" {
  ami = "ami-123456"
  instance_type = "t2.micro"
  key_name = "foo"
  count = 2

  provisioner "habitat" {
    peer = "${aws_instance.web.0.private_ip}"
    use_sudo = true

    service {
      name = "core/nginx"
      user_toml = "${file("conf/nginx.toml")}"
    }
  }
}

resource "aws_instance" "lb" {
  ami = "ami-123456"
  instance_type = "t2.micro"
  key_name = "foo"

  provisioner "habitat" {
    peer = "${aws_instance.web.0.private_ip}"
    use_sudo = true
    service_type = "systemd"
    service {
      name = "core/haproxy"
      binds = [
        "backend:nginx.default"
      ]
      user_toml = "${file("conf/haproxy.toml")}"
    }
  }
}
```

## Arguments
There are 2 configuration levels, supervisor and service.  Values placed directly within the `provisioner` block are supervisor configs, and values placed inside a `service` block are service configs.  Services can also take a `bind` block to configure runtime bindings.

### Supervisor
* `version`: The version of Habitat to install.  Optional (Defaults to latest)
* `permanent_peer`: Whether this supervisor should be marked as a permanent peer. Optional (Defaults to false)
* `listen_gossip`: IP and port to listen for gossip traffic.  Optional (Defaults to "0.0.0.0:9638")
* `listen_http`: IP and port for the HTTP API service.  Optional (Defaults to "0.0.0.0:9631")
* `peer`: IP or FQDN of a supervisor instance to peer with.  Optional (Defaults to none)
* `ring_key`: Key for encrypting the supervisor ring traffic.  Optional (Defaults to none)
* `skip_install`: Skips the installation Habitat, if it's being installed another way.  Optional (Defaults to no)
* `use_sudo`: Use sudo to execute commands on the target system. Optional (Defaults to false)
* `service_type`: Sets the type of hab-sup service you want to run.  Current options are `unmanaged` and `systemd`.  Defaults to `unmanaged`.
  * `unmanaged`: Uses `setsid` to kick off the habitat supervisor.  Not dependent on any init system.
  * `systemd`: Creates a systemd unit and starts the habitat supervisor service.  

### Service
* `name`: A package identifier of the Habitat package to start (eg `core/nginx`, `core/nginx/1.11.10` or `core/nginx/1.11.10/20170215233218`).  Required.
* `strategy`: Update strategy to use. Possible values "at-once", "rolling" or "none".  Optional (Defaults to "none")
* `topology`: Topology to start service in.  Possible values "standalone" or "leader".  Optional (Defaults to "standalone")
* `channel`: Channel in a remote depot to watch for package updates.  Optional
* `group`: Service group to join.  Optional (Defaults to "default")
* `url`: URL of the remote Depot to watch.  Optional (Defaults to the public depot)
* `binds`:  Array of binding statements (eg "backend:nginx.default").  Optional
* `user_toml`: TOML formatted user configuration for the service.  Easiest to source from a file (eg `user_toml = "${file("conf/redis.toml")}"`).  Optional

### Bind
* `alias`: The alias for the binding.
* `service`: The target service to bind.
* `group`: The target group to bind.

**This format for declaring bindings is optional.  It can be used in place of or along side the `binds = ["alias:service.group"]` method of declaring binds.  This format might be easier to manage when populating one or more of the bind parameters dynamically.

Example:
```
service {
  name = "core/haproxy"
  group = "${var.environment}"

  bind {
    alias = "backend"
    service = "nginx"
    group = "${var.environment}"
  }
}
```
This block will generate the option `--bind backend:nginx.default` when starting the haproxy service.


