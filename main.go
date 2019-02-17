package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/digitalocean/godo"
	"golang.org/x/oauth2"
)

type TokenSource struct {
	AccessToken string
}

type doClient struct {
	client *godo.Client
	ctx    context.Context
	dryRun bool
}

func (t *TokenSource) Token() (*oauth2.Token, error) {
	token := &oauth2.Token{
		AccessToken: t.AccessToken,
	}
	return token, nil
}

var s = spew.Dump

func main() {
	tokenSource := &TokenSource{
		AccessToken: os.Getenv("DIGITALOCEAN_TOKEN"),
	}

	ctx := context.TODO()
	d := &doClient{
		dryRun: false,
		ctx:    ctx,
		client: godo.NewClient(oauth2.NewClient(ctx, tokenSource)),
	}

	asgOpts := &createAsgOptions{
		name:     "test",
		region:   "sfo2",
		size:     "s-1vcpu-1gb",
		snapshot: "micropig-feb-16-2019-220616",
		count:    2,
	}
	drops, _ := d.createAsg(asgOpts)
	spew.Dump(drops)
}

func (d *doClient) getSnapshotBySlug(slug string) (int, error) {
	listOpts := &godo.ListOptions{
		Page:    1,
		PerPage: 200,
	}

	snapshots, _, err := d.client.Snapshots.ListDroplet(d.ctx, listOpts)
	if err != nil {
		return -1, err
	}

	if len(snapshots) <= 0 {
		return -1, errors.New("no snapshots found")
	}

	var snapshot godo.Snapshot

	for _, snap := range snapshots {
		if snap.Name == slug {
			snapshot = snap
			break
		}
	}
	//spew.Dump(snapshot)

	snapID, err := strconv.ParseInt(snapshot.ID, 0, 64)
	if err != nil {
		return -1, err
	}

	return int(snapID), nil
}

type createAsgOptions struct {
	name     string
	region   string
	size     string
	snapshot string
	count    int
}

type createDropletOptions struct {
	name_prefix string
	region      string
	size        string
	imageID     int
	index       int
}

func sshKeyIDs() []godo.DropletCreateSSHKey {
	return []godo.DropletCreateSSHKey{
		godo.DropletCreateSSHKey{ID: 24057133},
		godo.DropletCreateSSHKey{ID: 24057134},
	}
}

func (d *doClient) createAsg(opts *createAsgOptions) ([]godo.Droplet, error) {
	snapID, err := d.getSnapshotBySlug(opts.snapshot)
	if err != nil {
		return nil, err
	}

	c := make(chan *godo.Droplet)

	go func() {
		for i := 0; i < opts.count; i++ {
			createDropletOps := &createDropletOptions{
				name_prefix: opts.name,
				region:      opts.region,
				size:        opts.size,
				imageID:     snapID,
				index:       i,
			}
			drop := d.mustCreateDroplet(createDropletOps)
			c <- drop
		}
	}()

	droplets := make([]godo.Droplet, 0)

	for i := 0; i < opts.count; i++ {
		drop := <-c
		droplets = append(droplets, *drop)
	}

	return droplets, nil
}

func (d *doClient) mustCreateDroplet(opts *createDropletOptions) *godo.Droplet {
	t := time.Now()

	// name_prefix-region-ts-index
	name := fmt.Sprintf("%s-%s-%s-%d",
		opts.name_prefix,
		opts.region,
		t.Format("jan_22006T15-04-05Z"),
		opts.index,
	)

	createRequest := &godo.DropletCreateRequest{
		Name:   name,
		Region: opts.region,
		Size:   opts.size,
		Image: godo.DropletCreateImage{
			ID: opts.imageID,
		},
		SSHKeys:           sshKeyIDs(),
		PrivateNetworking: true,
		Monitoring:        true,
		UserData:          "",
		Tags:              []string{},
	}

	if d.dryRun {
		s(createRequest)
		return &godo.Droplet{}
	} else {
		drop, _, err := d.client.Droplets.Create(d.ctx, createRequest)
		if err != nil {
			panic(err)
		}
		return drop
	}
}
