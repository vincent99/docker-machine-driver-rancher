package rancher

import (
	"fmt"
	"io/ioutil"
	"strconv"
	"strings"
	"time"

	"github.com/docker/machine/libmachine/drivers"
	"github.com/docker/machine/libmachine/log"
	"github.com/docker/machine/libmachine/mcnflag"
	"github.com/docker/machine/libmachine/ssh"
	"github.com/docker/machine/libmachine/state"
	rancher "github.com/rancher/go-rancher/client"
)

type Driver struct {
	*drivers.BaseDriver
	Url       string
	AccessKey string
	SecretKey string

	OsImage  string
	MemoryMb int
	Vcpu     int
	//	RootDiskGb				int
	//	DataDiskGb				int

	client *rancher.RancherClient

	MachineId string
	IPAddress string
}

const (
	defaultOsImage  = "rancher/vm-ubuntu"
	defaultMemoryMb = 1024
	defaultVcpu     = 2
	//	defaultRootDiskGb= 20
	//	defaultDataDiskGb= 0
	clientMaxRetries = 5
)

// GetCreateFlags registers the flags this driver adds to
// "docker hosts create"
func (d *Driver) GetCreateFlags() []mcnflag.Flag {
	return []mcnflag.Flag{
		mcnflag.StringFlag{
			EnvVar: "RANCHER_URL",
			Name:   "rancher-url",
			Usage:  "Rancher Environment URL",
		},
		mcnflag.StringFlag{
			EnvVar: "RANCHER_ACCESS_KEY",
			Name:   "rancher-access-key",
			Usage:  "Rancher Access Key",
		},
		mcnflag.StringFlag{
			EnvVar: "RANCHER_SECRET_KEY",
			Name:   "rancher-secret-key",
			Usage:  "Rancher Secret Key",
		},
		mcnflag.StringFlag{
			EnvVar: "RANCHER_OS_IMAGE",
			Name:   "rancher-os-image",
			Usage:  "Rancher OS Image.  Default: " + defaultOsImage,
			Value:  defaultOsImage,
		},
		mcnflag.IntFlag{
			EnvVar: "RANCHER_MEMORY_MB",
			Name:   "rancher-memory-mb",
			Usage:  "Memory in MiB. Default: " + strconv.Itoa(defaultMemoryMb),
			Value:  defaultMemoryMb,
		},
		mcnflag.IntFlag{
			EnvVar: "RANCHER_VCPU",
			Name:   "rancher-vcpu",
			Usage:  "Number of virtual CPUs. Default: " + strconv.Itoa(defaultVcpu),
			Value:  defaultVcpu,
		},
		/*
			mcnflag.IntFlag{
				EnvVar: "RANCHER_ROOT_DISK_GB",
				Name:   "rancher-root-disk-gb",
				Usage:  "Root disk size in GiB. Default: " + strconv.Itoa(defaultRootDiskGb),
				Value:  defaultRootDiskGb,
			}
			mcnflag.IntFlag{
				EnvVar: "RANCHER_DATA_DISK_GB",
				Name:   "rancher-data-disk-gb",
				Usage:  "Root disk size in GiB. Default: " + strconv.Itoa(defaultDataDiskGb),
				Value:  defaultDataDiskGb,
			}
		*/
	}
}

func NewDriver(hostName, storePath string) *Driver {
	d := &Driver{
		OsImage:  defaultOsImage,
		MemoryMb: defaultMemoryMb,
		Vcpu:     defaultVcpu,
		//RootDiskGb:			defaultRootDiskGb,
		//DataDiskGb:			defaultDataDiskGb,

		BaseDriver: &drivers.BaseDriver{
			MachineName: hostName,
			StorePath:   storePath,
		},
	}
	return d
}

func (d *Driver) GetSSHHostname() (string, error) {
	return d.GetIP()
}

// DriverName returns the name of the driver
func (d *Driver) DriverName() string {
	return "rancher"
}

func (d *Driver) SetConfigFromFlags(flags drivers.DriverOptions) error {
	d.Url = flags.String("rancher-url")
	d.AccessKey = flags.String("rancher-access-key")
	d.SecretKey = flags.String("rancher-secret-key")
	d.OsImage = flags.String("rancher-os-image")
	d.MemoryMb = flags.Int("rancher-memory-mb")
	d.Vcpu = flags.Int("rancher-vcpu")
	//d.RootDiskGb = flags.Int("rancher-root-disk-gb")
	//d.DataDiskGb = flags.Int("rancher-data-disk-gb")

	if d.Url == "" {
		return fmt.Errorf("Rancher driver requires the --rancher-url option")
	}

	if d.AccessKey == "" {
		return fmt.Errorf("Rancher driver requires the --rancher-access-key option")
	}

	if d.SecretKey == "" {
		return fmt.Errorf("Rancher driver requires the --rancher-secret-key option")
	}

	return nil
}

func (d *Driver) PreCreateCheck() error {
	return nil
}

func (d *Driver) Create() error {
	log.Debug("Generating SSH key...")

	key, err := d.createSSHKey()
	if err != nil {
		return err
	}

	userdata := `#cloud-config

ssh_authorized_keys:
	- "` + key + `"`

	log.Info("Creating Rancher VM...")

	if userdata != "" {
		log.Infof("Using the following Cloud-init User Data:")
		log.Infof("%s", userdata)
	}

	client := d.getClient()

	machine, err := client.Create(&rancher.VirtualMachine{
		Name:      d.MachineName,
		ImageUuid: "docker:" + d.OsImage,
		MemoryMb:  int64(d.MemoryMb),
		Vcpu:      int64(d.Vcpu),
		Userdata:  userdata,
	})

	if err != nil {
		return err
	}

	d.MachineId = machine.Id

	log.Infof("Waiting for VM %s to become available...", d.MachineId)
	for {
		machine, err = d.getMachine()
		if err != nil {
			return err
		}

		d.IPAddress = machine.PrimaryIpAddress
		if d.IPAddress != "" && machine.State == "running" {
			break
		}

		log.Infof("VM not yet available, state=%s ip=%s", machine.State, machine.PrimaryIpAddress)
		time.Sleep(2 * time.Second)
	}

	log.Infof("Created Rancher VM ID: %s, Public IP: %s",
		d.MachineId,
		d.IPAddress,
	)

	return nil
}

func (d *Driver) createSSHKey() (string, error) {
	if err := ssh.GenerateSSHKey(d.GetSSHKeyPath()); err != nil {
		return "", err
	}

	publicKey, err := ioutil.ReadFile(d.publicSSHKeyPath())
	if err != nil {
		return "", err
	}

	return strings.TrimRight(string(publicKey), "\r\n\t "), nil
}

func (d *Driver) GetURL() (string, error) {
	s, err := d.GetState()
	if err != nil {
		return "", err
	}

	if s != state.Running {
		return "", drivers.ErrHostIsNotRunning
	}

	ip, err := d.GetIP()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("tcp://%s:2376", ip), nil
}

func (d *Driver) GetIP() (string, error) {
	if d.IPAddress == "" || d.IPAddress == "0" {
		return "", fmt.Errorf("IP address is not set")
	}
	return d.IPAddress, nil
}

func (d *Driver) getMachine() (*rancher.VirtualMachine, error) {
	return d.getClient().ById(d.MachineId)
}

func (d *Driver) GetState() (state.State, error) {
	machine, err := d.getMachine()

	if err != nil {
		return state.Error, err
	}

	switch machine.State {
	case "creating", "migrating", "requested", "restarting", "restoring", "starting":
		return state.Starting, nil

	case "error", "erroring", "purged", "purging", "removed", "removing":
		return state.Error, nil

	case "running", "updating-running":
		return state.Running, nil

	case "stopped", "updating-stopped":
		return state.Stopped, nil

	case "stopping":
		return state.Stopping, nil

	default:
		return state.None, nil
	}
}

func (d *Driver) Start() error {
	machine, err := d.getMachine()
	if err != nil {
		return err
	}

	log.Infof("Starting %s", d.MachineName)
	_, err = d.getClient().ActionStart(machine)
	return err
}

func (d *Driver) Stop() error {
	machine, err := d.getMachine()
	if err != nil {
		return err
	}

	log.Infof("Stopping %s", d.MachineName)
	_, err = d.getClient().ActionStop(machine, &rancher.InstanceStop{})
	return err
}

func (d *Driver) Remove() error {
	machine, err := d.getMachine()
	if err != nil {
		return err
	}

	log.Infof("Removing %s", d.MachineName)
	return d.getClient().Delete(machine)
}

func (d *Driver) Restart() error {
	machine, err := d.getMachine()
	if err != nil {
		return err
	}

	log.Infof("Stopping %s", d.MachineName)
	_, err = d.getClient().ActionRestart(machine)
	return err
}

func (d *Driver) Kill() error {
	return d.Stop()
}

func (d *Driver) getClient() rancher.VirtualMachineOperations {
	if d.client == nil {
		client, err := rancher.NewRancherClient(&rancher.ClientOpts{
			Url:       d.Url,
			AccessKey: d.AccessKey,
			SecretKey: d.SecretKey,
		})

		if err != nil {
			return nil
		}

		d.client = client
	}

	return d.client.VirtualMachine
}

func (d *Driver) publicSSHKeyPath() string {
	return d.GetSSHKeyPath() + ".pub"
}
