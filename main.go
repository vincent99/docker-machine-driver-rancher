package main

import (
	"github.com/docker/machine/libmachine/drivers/plugin"
	"github.com/vincent99/docker-machine-driver-rancher/rancher"
)

var Version string

func main() {
	plugin.RegisterDriver(rancher.NewDriver("", ""))
}
