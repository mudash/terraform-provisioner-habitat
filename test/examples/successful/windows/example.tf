provider "aws" {
  region     = "${var.region}"
  access_key = "${var.access_key}"
}

resource "aws_instance" "ms-hab-win-vm" {
  ami = "${lookup(var.win_amis, var.region)}"
  instance_type = "t2.micro"
  key_name = "${var.key_name}"
  tags = {
    Name = "ms-hab-win-vm"
  }
  user_data = <<EOF
  <powershell>
  net user ${var.win_username} '${var.win_password}' /add /y
  net localgroup administrators ${var.win_username} /add
  winrm quickconfig -q
  winrm set winrm/config/winrs '@{MaxMemoryPerShellMB="300"}'
  winrm set winrm/config '@{MaxTimeoutms="1800000"}'
  winrm set winrm/config/service '@{AllowUnencrypted="true"}'
  winrm set winrm/config/service/auth '@{Basic="true"}'
  netsh advfirewall firewall add rule name="WinRM 5985" protocol=TCP dir=in localport=5985 action=allow
  netsh advfirewall firewall add rule name="WinRM 5986" protocol=TCP dir=in localport=5986 action=allow
  net stop winrm
  sc.exe config winrm start=auto
  net start winrm
  </powershell>
  EOF

  provisioner "habitat_dev" {
    peer = ""
    
    service {
      name = "mwrock/hab-sln"
      topology = "standalone"
      user_toml = "${file("conf/user.toml")}"
    }

    service {
      name = "mwrock/blank1"
      topology = "standalone"
      user_toml = ""
    }

    connection {
      type = "winrm"
      timeout = "10m"
      user = "${var.win_username}"
      password = "${var.win_password}"
    }
  }
}
output "ips" {
  value = ["${aws_instance.ms-hab-win-vm.public_ip}"]
}

output "username" {
  value = "${var.win_username}"
}

output "password" {
  value = "${var.win_password}"
}

