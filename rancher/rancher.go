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
	OsUser   string
	MemoryMb int
	Vcpu     int
	//	RootDiskGb				int
	//	DataDiskGb				int

	client *rancher.RancherClient

	ProjectName string
	ProjectId   string
	MachineId   string
	IPAddress   string
}

const (
	defaultOsImage  = "rancher/vm-ubuntu"
	defaultOsUser   = "ubuntu"
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
			Usage:  "Rancher API URL",
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
			EnvVar: "RANCHER_ENVIRONMENT_NAME",
			Name:   "rancher-project-name",
			Usage:  "Rancher Environment Name (Name or ID are required if API key has access to more than one Environment",
		},
		mcnflag.StringFlag{
			EnvVar: "RANCHER_ENVIRONMENT_ID",
			Name:   "rancher-project-id",
			Usage:  "Rancher Environment ID (Name or ID are required if API key has access to more than one Environment)",
		},
		mcnflag.StringFlag{
			EnvVar: "RANCHER_OS_IMAGE",
			Name:   "rancher-os-image",
			Usage:  "Rancher OS Image.  Default: " + defaultOsImage,
			Value:  defaultOsImage,
		},
		mcnflag.StringFlag{
			EnvVar: "RANCHER_OS_USER",
			Name:   "rancher-os-user",
			Usage:  "Rancher OS User.  Default: " + defaultOsUser,
			Value:  defaultOsUser,
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
	d.OsUser = flags.String("rancher-os-user")
	d.MemoryMb = flags.Int("rancher-memory-mb")
	d.Vcpu = flags.Int("rancher-vcpu")
	d.ProjectName = flags.String("rancher-project-name")
	d.ProjectId = flags.String("rancher-project-id")
	//d.RootDiskGb = flags.Int("rancher-root-disk-gb")
	//d.DataDiskGb = flags.Int("rancher-data-disk-gb")

	if d.Url == "" {
		return fmt.Errorf("Rancher driver requires the --rancher-url option")
	}

	return nil
}

func (d *Driver) PreCreateCheck() error {
	project, err := d.selectProject()
	if err != nil {
		return err
	}

	d.client = nil
	d.Url = project.Links["self"]
	log.Debugf("Set URL to %s", d.Url)

	return nil
}

func (d *Driver) selectProject() (*rancher.Project, error) {
	var err error

	client := d.getClient()

	// Specific project ID
	if d.ProjectId != "" {
		return client.Project.ById(d.ProjectId)
	}

	// Almost-specific project name
	if d.ProjectName != "" {
		projects, err := client.Project.List(&rancher.ListOpts{
			Filters: map[string]interface{}{
				"name":     d.ProjectName,
				"state_ne": "removed",
				"limit":    "2",
			},
		})

		if err != nil {
			return nil, err
		}

		if len(projects.Data) > 1 {
			return nil, fmt.Errorf("There is more than one Environment named '%s', use --rancher-environment-id to choose one", d.ProjectName)
		}

		if len(projects.Data) == 0 {
			return nil, fmt.Errorf("No Environment named '%s' was found, check URL and API key", d.ProjectName)
		}

		return &projects.Data[0], nil
	}

	// Guess
	projects, err := client.Project.List(&rancher.ListOpts{
		Filters: map[string]interface{}{
			"state_ne": "removed",
			"limit":    "2",
		},
	})

	if err != nil {
		return nil, err
	}

	if len(projects.Data) > 1 {
		return nil, fmt.Errorf("The supplied API Key has access to more than one Environment, use --rancher-environment-id to choose one")
	}

	if len(projects.Data) == 0 {
		return nil, fmt.Errorf("No Environments found, check URL and API key")
	}

	return &projects.Data[0], nil
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

	machine, err := d.getClient().VirtualMachine.Create(&rancher.VirtualMachine{
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

func (d *Driver) GetSSHUsername() string {
	if d.OsUser == "" {
		d.OsUser = defaultOsUser
	}
	return d.OsUser
}

func (d *Driver) getMachine() (*rancher.VirtualMachine, error) {
	return d.getClient().VirtualMachine.ById(d.MachineId)
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
	_, err = d.getClient().VirtualMachine.ActionStart(machine)
	return err
}

func (d *Driver) Stop() error {
	machine, err := d.getMachine()
	if err != nil {
		return err
	}

	log.Infof("Stopping %s", d.MachineName)
	_, err = d.getClient().VirtualMachine.ActionStop(machine, &rancher.InstanceStop{})
	return err
}

func (d *Driver) Remove() error {
	machine, err := d.getMachine()
	if err != nil {
		return err
	}

	log.Infof("Removing %s", d.MachineName)
	return d.getClient().VirtualMachine.Delete(machine)
}

func (d *Driver) Restart() error {
	machine, err := d.getMachine()
	if err != nil {
		return err
	}

	log.Infof("Stopping %s", d.MachineName)
	_, err = d.getClient().VirtualMachine.ActionRestart(machine)
	return err
}

func (d *Driver) Kill() error {
	return d.Stop()
}

func (d *Driver) getClient() *rancher.RancherClient {
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

	return d.client
}

func (d *Driver) publicSSHKeyPath() string {
	return d.GetSSHKeyPath() + ".pub"
}
