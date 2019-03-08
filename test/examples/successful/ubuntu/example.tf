provider "aws" {
  region     = "${var.region}"
  access_key = "${var.access_key}"
}

resource "aws_instance" "ms-hab-ubuntu-vm" {
  ami           = "ami-0f9cf087c1f27d9b1"
  instance_type = "t2.micro"
  key_name = "${var.key_name}"
  tags = {
    Name = "ms-hab-ubuntu-vm"
  }
  provisioner "habitat_dev" {
    peer = ""
    use_sudo = true
    service_type = "systemd"

    service {
      name = "core/nginx"
      topology = "standalone"
      user_toml = ""
    }

    connection {
      type     = "ssh"
      user = "ubuntu"
      private_key = "${file("${var.key_path}")}"
    }
  }
}
output "ips" {
  value = ["${aws_instance.ms-hab-ubuntu-vm.public_ip}"]
}

output "username" {
  value = "ubuntu"
}

output "key_path" {
  value = "${var.key_path}"
}

