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
		desired:  2,
		min:      0,
		max:      2,
	}
	asg, _ := d.createAsg(asgOpts)
	s(asg)
	d.waitForAsgStatus(asg, "ok")
	s(asg)
}

func (d *doClient) getSnapshotIdBySlug(slug string) (int, error) {
	listOpts := &godo.ListOptions{Page: 1, PerPage: 200}

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
	desired  int
	min      int
	max      int
}

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

type micropigAsg struct {
	name     string
	status   string
	droplets []godo.Droplet
	desired  int
	min      int
	max      int
}

func (d *doClient) createAsg(opts *createAsgOptions) (*micropigAsg, error) {
	if opts.min > opts.desired || opts.min > opts.max || opts.desired > opts.max {
		return nil, errors.New("min <= desired <= max")
	}

	t := time.Now()

	// name-region-ts-index
	asgName := fmt.Sprintf("%s-%s-%s", opts.name, opts.region, t.Format("jan_22006T15-04-05Z"))
	asgTag := "asg:" + asgName

	snapID, err := d.getSnapshotIdBySlug(opts.snapshot)
	if err != nil {
		return nil, err
	}

	c := make(chan *godo.Droplet)

	go func() {
		for i := 0; i < opts.desired; i++ {
			createDropletOps := &createDropletOptions{
				name:    fmt.Sprintf("%s-%d", asgName, i),
				region:  opts.region,
				size:    opts.size,
				imageID: snapID,
				tags:    []string{"all", asgTag},
			}
			drop := d.mustCreateDroplet(createDropletOps)
			c <- drop
		}
	}()

	droplets := make([]godo.Droplet, 0)

	// wait for the droplets to be created in parallel
	for i := 0; i < opts.desired; i++ {
		drop := <-c
		droplets = append(droplets, *drop)
	}

	asg := &micropigAsg{
		status:   "new",
		name:     asgName,
		droplets: droplets,
		desired:  opts.desired,
		min:      opts.min,
		max:      opts.max,
	}

	return asg, nil
}

func (d *doClient) waitForAsgStatus(asg *micropigAsg, status string) error {
	timeout := time.Duration(300) // 5m
	c := make(chan bool)

	go func() {
		for {
			d.updateAsgStatus(asg)
			if asg.status == status {
				break
			}
		}
		c <- true
	}()

	select {
	case <-c:
		return nil
	case <-time.After(timeout * time.Second):
		return errors.New("timeout waiting for asg status=" + status)
	}
}

func (d *doClient) updateAsgStatus(asg *micropigAsg) error {
	listOpts := &godo.ListOptions{Page: 1, PerPage: 200}
	droplets, _, err := d.client.Droplets.ListByTag(d.ctx, "asg:"+asg.name, listOpts)

	if err != nil {
		return err
	}

	allActive := true
	asg.droplets = droplets
	currentSize := len(asg.droplets)

	for _, drop := range asg.droplets {
		status := drop.Status
		if status != "active" {
			allActive = false
			break
		}
	}

	if currentSize == asg.desired {
		if allActive {
			asg.status = "ok"
		} else {
			asg.status = "not_active"
		}
	} else if currentSize < asg.desired {
		asg.status = "under"
	} else if currentSize > asg.desired {
		asg.status = "over"
	} else {
		return errors.New("unknown problem with asg")
	}

	return nil
}

func (d *doClient) mustCreateDroplet(opts *createDropletOptions) *godo.Droplet {
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
