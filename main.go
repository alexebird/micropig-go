package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/alexebird/tableme/tableme"
	"github.com/davecgh/go-spew/spew"
	"github.com/digitalocean/godo"
	"github.com/urfave/cli"
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

type micropigAsg struct {
	name     string
	status   string
	droplets []godo.Droplet
	desired  int
	min      int
	max      int
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

	app := cli.NewApp()
	app.Name = "micropig"
	app.Usage = "oink"

	app.Commands = []cli.Command{
		{
			Name:  "asg",
			Usage: "asg operations",
			Subcommands: []cli.Command{
				{
					Name:  "create",
					Usage: "create an asg",
					Flags: []cli.Flag{
						cli.StringFlag{Name: "name, n"},
						cli.StringFlag{Name: "snapshot, i"},
						cli.StringFlag{Name: "region, r"},
						cli.StringFlag{Name: "size, s"},
						cli.StringFlag{Name: "type, t"},
						cli.BoolFlag{Name: "wait"},
						cli.IntFlag{Name: "max"},
						cli.IntFlag{Name: "min"},
						cli.IntFlag{Name: "desired"},
					},
					Action: func(c *cli.Context) error {
						name := c.String("name")
						fmt.Println("creating asg name=" + name)
						asgOpts := &createAsgOptions{
							name:     name,
							region:   c.String("region"),
							size:     c.String("size"),
							snapshot: c.String("snapshot"),
							desired:  c.Int("desired"),
							min:      c.Int("min"),
							max:      c.Int("max"),
						}
						asg, err := d.createAsg(asgOpts)
						if err != nil {
							panic(err)
						}
						fmt.Println("created asg name=" + asg.name)
						//s(asg)
						fmt.Println("waiting for asg name=" + asg.name)
						d.waitForAsgStatus(asg, "ok")
						fmt.Println("asg ok name=" + asg.name)
						//s(asg)
						return nil
					},
				},
				{
					Name:  "ls",
					Usage: "list asgs",
					Action: func(c *cli.Context) error {
						asgs, err := d.listAsgs()
						if err != nil {
							panic(err)
						}

						var records [][]string
						for _, asg := range asgs {
							records = append(records, []string{
								asg.name,
								strconv.Itoa(len(asg.droplets)),
							})
						}
						bites := tableme.TableMe([]string{"NAME", "CURR"}, records)
						buff := bytes.NewBuffer(bites)
						fmt.Print(buff.String())

						//s(asgs)
						return nil
					},
				},
				{
					Name:  "rm",
					Usage: "rm asg",
					Flags: []cli.Flag{
						cli.StringFlag{Name: "name, n"},
					},
					Action: func(c *cli.Context) error {
						name := c.String("name")
						err := d.rmAsg(name)
						if err != nil {
							panic(err)
						}

						return nil
					},
				},
			},
		},
	}

	err := app.Run(os.Args)
	if err != nil {
		panic(err)
	}

}

func sshKeyIDs() []godo.DropletCreateSSHKey {
	return []godo.DropletCreateSSHKey{
		godo.DropletCreateSSHKey{ID: 24057133},
		godo.DropletCreateSSHKey{ID: 24057134},
	}
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

func (d *doClient) createAsg(opts *createAsgOptions) (*micropigAsg, error) {
	if opts.min > opts.desired || opts.min > opts.max || opts.desired > opts.max {
		return nil, errors.New("min <= desired <= max")
	}

	t := time.Now().UTC()

	// name-region-ts-index
	asgName := fmt.Sprintf("%s-%s-%s", opts.name, opts.region, t.Format("jan_22006T15-04-05Z"))

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
				tags: []string{
					"all",
					"master",
					"minion",
					"asg:" + asgName,
					//"asg_min:" + strconv.Itoa(opts.min),
					//"asg_desired:" + strconv.Itoa(opts.desired),
					//"asg_max:" + strconv.Itoa(opts.max),
					//"asg_region:" + opts.region,
					//"asg_size:" + opts.size,
					//"asg_image:" + snapID,
				},
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
			drops, _ := d.listAsgDroplets(asg)
			d.updateAsgStatus(asg, drops)
			if asg.status == status {
				c <- true
				break
			}
			time.Sleep(5 * time.Second)
		}
	}()

	select {
	case <-c:
		return nil
	case <-time.After(timeout * time.Second):
		return errors.New("timeout waiting for asg status=" + status)
	}
}

func (d *doClient) listAsgDroplets(asg *micropigAsg) ([]godo.Droplet, error) {
	listOpts := &godo.ListOptions{Page: 1, PerPage: 200}
	droplets, _, err := d.client.Droplets.ListByTag(d.ctx, "asg:"+asg.name, listOpts)
	if err != nil {
		return nil, err
	}
	return droplets, nil
}

func (d *doClient) updateAsgStatus(asg *micropigAsg, droplets []godo.Droplet) error {
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
		} else if currentSize == 0 {
			asg.status = "empty"
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

func (d *doClient) mustDeleteDroplet(dropletID int) {
	if d.dryRun {
		s(dropletID)
	} else {
		_, err := d.client.Droplets.Delete(d.ctx, dropletID)
		if err != nil {
			panic(err)
		}
	}
}

func getTagValueS(key string, tags []string) string {
	for _, tag := range tags {
		if strings.HasPrefix(tag, key) {
			parts := strings.SplitN(tag, ":", 2)
			return parts[1]
		}
	}
	panic(errors.New("couldnt find droplet tag"))
}

func getTagValueI(key string, tags []string) int {
	val := getTagValueS(key, tags)
	valI, err := strconv.ParseInt(val, 0, 64)
	if err != nil {
		panic(err)
	}
	return int(valI)
}

func (d *doClient) getAsg(name string) (*micropigAsg, error) {
	asgs, err := d.listAsgsWithName(&name)
	if err != nil {
		return nil, err
	}
	if len(asgs) == 0 {

		return nil, nil
	}
	return &asgs[0], nil
}

func (d *doClient) listAsgs() ([]micropigAsg, error) {
	asgs, err := d.listAsgsWithName(nil)
	if err != nil {
		return nil, err
	}
	return asgs, nil
}

func (d *doClient) listAsgsWithName(name *string) ([]micropigAsg, error) {
	asgs := make([]micropigAsg, 0)
	instanceMap := make(map[string][]godo.Droplet, 0)

	listOpts := &godo.ListOptions{Page: 1, PerPage: 200}
	droplets, _, err := d.client.Droplets.List(d.ctx, listOpts)
	if err != nil {
		return nil, err
	}

	for _, drop := range droplets {
		asgName := getTagValueS("asg", drop.Tags)
		if name == nil || (name != nil && asgName == *name) {
			if instanceMap[asgName] != nil {
				instanceMap[asgName] = append(instanceMap[asgName], drop)
			} else {
				instanceMap[asgName] = []godo.Droplet{drop}
			}
		}
	}

	for asgName, drops := range instanceMap {
		asg := &micropigAsg{
			name:     asgName,
			droplets: drops,
			//desired:  getTagValueI("asg_desired", drops[0].Tags),
			//min:      getTagValueI("asg_min", drops[0].Tags),
			//max:      getTagValueI("asg_max", drops[0].Tags),
		}
		d.updateAsgStatus(asg, drops)
		asgs = append(asgs, *asg)
	}

	return asgs, nil
}

func (d *doClient) scaleToDesired(asg *micropigAsg) error {
	fmt.Println("scaling asg name=" + asg.name + " to " + strconv.Itoa(asg.desired))
	currentSize := len(asg.droplets)

	if currentSize == asg.desired {
		if currentSize == 0 {
			return nil
		} else {
			return nil
		}
		//} else if currentSize < asg.desired { // TODO
		//d.scaleUpAsIs(asg)
	} else if currentSize > asg.desired {
		d.scaleDownAsIs(asg)
	} else {
		return errors.New("unknown problem with asg")
	}
	return nil
}

//func (d *doClient) scaleUpAsIs(asg *micropigAsg) error {
//drop := d.mustCreateDroplet(createDropletOps)
//return nil
//}

// just take the current size diff and take that action.
func (d *doClient) scaleDownAsIs(asg *micropigAsg) error {
	rmCount := len(asg.droplets) - asg.desired
	for i := 0; i < rmCount; i++ {
		dropID := asg.droplets[i].ID
		fmt.Println("deleting droplet id=" + strconv.Itoa(dropID))
		d.mustDeleteDroplet(dropID)
	}
	return nil
}

func (d *doClient) rmAsg(name string) error {
	fmt.Println("removing asg name=" + name)
	asg, err := d.getAsg(name)
	if err != nil {
		return err
	}

	if asg != nil {
		asg.desired = 0

		err = d.scaleToDesired(asg)
		if err != nil {
			return err
		}
	}

	return nil
}
