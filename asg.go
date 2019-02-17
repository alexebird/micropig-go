package main

import (
	//"bytes"
	//"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	//"github.com/alexebird/tableme/tableme"

	"github.com/digitalocean/godo"
	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/postgres"
)

type createAsgOptions struct {
	name     string
	region   string
	size     string
	snapshot string
	desired  int
	min      int
	max      int
}

type micropigAsg struct {
	name     string
	status   string
	droplets []godo.Droplet
	desired  int
	min      int
	max      int
}

type Asg struct {
	gorm.Model
	Name     string `gorm:"unique;not null"`
	Status   string
	Droplets []godo.Droplet `gorm:"-"`
	Desired  int
	Min      int
	Max      int
}

func (m *Micropig) createAsg(opts *createAsgOptions) (*micropigAsg, error) {
	if opts.min > opts.desired || opts.min > opts.max || opts.desired > opts.max {
		return nil, errors.New("min <= desired <= max")
	}

	t := time.Now().UTC()

	// name-region-ts-index
	asgName := fmt.Sprintf("%s-%s-%s", opts.name, opts.region, t.Format("jan_22006T15-04-05Z"))

	snapID, err := m.getSnapshotIdBySlug(opts.snapshot)
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
			drop := m.mustCreateDroplet(createDropletOps)
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

func (m *Micropig) waitForAsgStatus(asg *micropigAsg, status string) error {
	timeout := time.Duration(300) // 5m
	c := make(chan bool)

	go func() {
		for {
			drops, _ := m.listAsgDroplets(asg)
			m.updateAsgStatus(asg, drops)
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

func (m *Micropig) listAsgDroplets(asg *micropigAsg) ([]godo.Droplet, error) {
	listOpts := &godo.ListOptions{Page: 1, PerPage: 200}
	droplets, _, err := m.DoClient.Droplets.ListByTag(m.Ctx, "asg:"+asg.name, listOpts)
	if err != nil {
		return nil, err
	}
	return droplets, nil
}

func (m *Micropig) updateAsgStatus(asg *micropigAsg, droplets []godo.Droplet) error {
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

func (m *Micropig) getAsg(name string) (*micropigAsg, error) {
	asgs, err := m.listAsgsWithName(&name)
	if err != nil {
		return nil, err
	}
	if len(asgs) == 0 {

		return nil, nil
	}
	return &asgs[0], nil
}

func (m *Micropig) listAsgs() ([]micropigAsg, error) {
	asgs, err := m.listAsgsWithName(nil)
	if err != nil {
		return nil, err
	}
	return asgs, nil
}

func (m *Micropig) listAsgsWithName(name *string) ([]micropigAsg, error) {
	asgs := make([]micropigAsg, 0)
	instanceMap := make(map[string][]godo.Droplet, 0)

	listOpts := &godo.ListOptions{Page: 1, PerPage: 200}
	droplets, _, err := m.DoClient.Droplets.List(m.Ctx, listOpts)
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
		m.updateAsgStatus(asg, drops)
		asgs = append(asgs, *asg)
	}

	return asgs, nil
}

func (m *Micropig) scaleToDesired(asg *micropigAsg) error {
	fmt.Println("scaling asg name=" + asg.name + " to " + strconv.Itoa(asg.desired))
	currentSize := len(asg.droplets)

	if currentSize == asg.desired {
		if currentSize == 0 {
			return nil
		} else {
			return nil
		}
		//} else if currentSize < asg.desired { // TODO
		//m.scaleUpAsIs(asg)
	} else if currentSize > asg.desired {
		m.scaleDownAsIs(asg)
	} else {
		return errors.New("unknown problem with asg")
	}
	return nil
}

//func (m *Micropig) scaleUpAsIs(asg *micropigAsg) error {
//drop := m.mustCreateDroplet(createDropletOps)
//return nil
//}

// just take the current size diff and take that action.
func (m *Micropig) scaleDownAsIs(asg *micropigAsg) error {
	rmCount := len(asg.droplets) - asg.desired
	for i := 0; i < rmCount; i++ {
		dropID := asg.droplets[i].ID
		fmt.Println("deleting droplet id=" + strconv.Itoa(dropID))
		m.mustDeleteDroplet(dropID)
	}
	return nil
}

func (m *Micropig) rmAsg(name string) error {
	fmt.Println("removing asg name=" + name)
	asg, err := m.getAsg(name)
	if err != nil {
		return err
	}

	if asg != nil {
		asg.desired = 0

		err = m.scaleToDesired(asg)
		if err != nil {
			return err
		}
	}

	return nil
}
