package main

import (
	//"bytes"

	//"github.com/alexebird/tableme/tableme"

	"fmt"

	"github.com/digitalocean/godo"
	_ "github.com/jinzhu/gorm/dialects/postgres"
)

type CreateDropletOptions struct {
	Name     string
	Region   string
	Size     string
	ImageID  int
	Tags     []string
	UserData string
}

func sshKeyIDs() []godo.DropletCreateSSHKey {
	return []godo.DropletCreateSSHKey{
		godo.DropletCreateSSHKey{ID: 24057133},
		godo.DropletCreateSSHKey{ID: 24057134},
	}
}

func (m *Micropig) MustCreateDroplet(opts *CreateDropletOptions) *godo.Droplet {
	createRequest := &godo.DropletCreateRequest{
		Name:              opts.Name,
		Region:            opts.Region,
		Size:              opts.Size,
		Image:             godo.DropletCreateImage{ID: opts.ImageID},
		SSHKeys:           sshKeyIDs(),
		PrivateNetworking: true,
		Monitoring:        true,
		UserData:          opts.UserData,
		Tags:              opts.Tags,
	}

	if m.DryRun {
		fmt.Printf("[DRY] creating droplet %s\n", opts.Name)
		return &godo.Droplet{}
	} else {
		drop, _, err := m.DoClient.Droplets.Create(m.Ctx, createRequest)
		if err != nil {
			panic(err)
		}
		return drop
	}
}

func (m *Micropig) MustDeleteDroplet(dropletID int) {
	if m.DryRun {
		fmt.Printf("[DRY] deleting droplet %d\n", dropletID)
	} else {
		_, err := m.DoClient.Droplets.Delete(m.Ctx, dropletID)
		if err != nil {
			panic(err)
		}
	}
}
