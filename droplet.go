package main

import (
	//"bytes"

	//"github.com/alexebird/tableme/tableme"

	"github.com/digitalocean/godo"
	_ "github.com/jinzhu/gorm/dialects/postgres"
)

type createDropletOptions struct {
	name    string
	region  string
	size    string
	imageID int
	tags    []string
}

func sshKeyIDs() []godo.DropletCreateSSHKey {
	return []godo.DropletCreateSSHKey{
		godo.DropletCreateSSHKey{ID: 24057133},
		godo.DropletCreateSSHKey{ID: 24057134},
	}
}

func (m *Micropig) mustCreateDroplet(opts *createDropletOptions) *godo.Droplet {
	createRequest := &godo.DropletCreateRequest{
		Name:              opts.name,
		Region:            opts.region,
		Size:              opts.size,
		Image:             godo.DropletCreateImage{ID: opts.imageID},
		SSHKeys:           sshKeyIDs(),
		PrivateNetworking: true,
		Monitoring:        true,
		UserData:          "",
		Tags:              opts.tags,
	}

	if m.DryRun {
		s(createRequest)
		return &godo.Droplet{}
	} else {
		drop, _, err := m.DoClient.Droplets.Create(m.Ctx, createRequest)
		if err != nil {
			panic(err)
		}
		return drop
	}
}

func (m *Micropig) mustDeleteDroplet(dropletID int) {
	if m.DryRun {
		s(dropletID)
	} else {
		_, err := m.DoClient.Droplets.Delete(m.Ctx, dropletID)
		if err != nil {
			panic(err)
		}
	}
}
