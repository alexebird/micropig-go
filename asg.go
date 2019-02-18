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
	_ "github.com/jinzhu/gorm/dialects/postgres"
)

type CreateAsgOptions struct {
	Name     string
	Region   string
	Size     string
	Snapshot string
	Tags     []string
	Min      int
	Desired  int
	Max      int
}

type Asg struct {
	ID        uint `gorm:"primary_key"`
	CreatedAt time.Time
	UpdatedAt time.Time
	Status    string         `gorm:"-"`
	Droplets  []godo.Droplet `gorm:"-"`
	Name      string         `gorm:"unique;not null"`
	Region    string
	Size      string
	TagsStr   string
	ImageID   int
	ImageSlug string
	Min       int
	Desired   int
	Max       int
}

func (a *Asg) CurrentSize() int {
	return len(a.Droplets)
}

func (a *Asg) Tags() []string {
	return append(deserializeTags(a.TagsStr), a.IDTag())
}

func (a *Asg) IDTag() string {
	return fmt.Sprintf("asg:%d", a.ID)
}

func validateAsgCounts(min int, desired int, max int) bool {
	return (min <= desired && desired <= max)
}

func (m *Micropig) CreateAsg(opts *CreateAsgOptions) (*Asg, error) {
	if !validateAsgCounts(opts.Min, opts.Desired, opts.Max) {
		return nil, errors.New("required: Min <= Desired <= Max")
	}

	t := time.Now().UTC()
	asgName := fmt.Sprintf("%s-%s-%s", opts.Name, opts.Region, t.Format("jan_22006T15-04-05Z"))

	snapID, err := m.GetSnapshotIdBySlug(opts.Snapshot)
	if err != nil {
		return nil, err
	}

	asg := &Asg{
		Status:    "new",
		Droplets:  []godo.Droplet{},
		Name:      asgName,
		Region:    opts.Region,
		Size:      opts.Size,
		TagsStr:   serializeTags(opts.Tags),
		ImageID:   snapID,
		ImageSlug: opts.Snapshot,
		Min:       opts.Min,
		Desired:   opts.Desired,
		Max:       opts.Max,
	}

	if err := m.Db.Create(asg).Error; err != nil {
		return nil, err
	}

	return asg, nil
}

func (m *Micropig) SetAsgDesired(asg *Asg, desired int) error {
	if !validateAsgCounts(asg.Min, desired, asg.Max) {
		return errors.New("required: Min <= Desired <= Max")
	}

	asg.Desired = desired
	m.Db.Save(asg)
	return nil
}

func (m *Micropig) GetAsgByName(name string) (*Asg, error) {
	var asg Asg
	err := m.Db.First(&asg, "name = ?", name).Error
	if err != nil {
		return nil, err
	}
	return &asg, nil
}

func (m *Micropig) DeleteAsgByName(name string) error {
	asg, err := m.GetAsgByName(name)
	if err != nil {
		return err
	}

	err = m.SetAsgDesired(asg, 0)
	if err != nil {
		return err
	}

	err = m.ScaleToDesired(asg)
	if err != nil {
		return err
	}

	m.WaitForAsgStatus(asg, "empty")

	if !m.Db.NewRecord(asg) {
		err = m.Db.Delete(asg).Error
		if err != nil {
			return err
		}
	}

	return nil
}

func (m *Micropig) WaitForAsgStatus(asg *Asg, status string) error {
	timeout := time.Duration(300) // 5m
	c := make(chan bool)

	go func() {
		for {
			m.updateAsg(asg)
			if asg.Status == status {
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

func (m *Micropig) ScaleToDesired(asg *Asg) error {
	fmt.Println("scaling asg name=" + asg.Name + " to " + strconv.Itoa(asg.Desired))
	m.updateAsg(asg)
	currentSize := asg.CurrentSize()

	if currentSize == asg.Desired {
		if currentSize == 0 { // be explicit
			return nil
		} else {
			return nil
		}
	} else if currentSize < asg.Desired {
		return m.scaleUpSimple(asg)
	} else if currentSize > asg.Desired {
		return m.scaleDownSimple(asg)
	} else {
		panic(errors.New("this should never happen"))
	}
	return nil
}

func getHighestIndex(drops []godo.Droplet) int {
	if len(drops) == 0 {
		return -1
	}

	var maxID int64 = -1

	for _, drop := range drops {
		nameParts := strings.Split(drop.Name, "-")
		id, err := strconv.ParseInt(nameParts[len(nameParts)-1], 0, 64)
		if err != nil {
			panic(err)
		}
		if id > maxID {
			maxID = id
		}
	}

	return int(maxID)
}

func (m *Micropig) scaleUpSimple(asg *Asg) error {
	c := make(chan *godo.Droplet)
	droplets := make([]godo.Droplet, 0)
	createCount := asg.Desired - asg.CurrentSize()
	startingIdx := getHighestIndex(asg.Droplets) + 1

	for i := startingIdx; i < (startingIdx + createCount); i++ {
		go func(i int) {
			createDropletOps := &CreateDropletOptions{
				Name:    fmt.Sprintf("%s-%d", asg.Name, i),
				Region:  asg.Region,
				Size:    asg.Size,
				ImageID: asg.ImageID,
				Tags:    asg.Tags(),
			}
			drop := m.MustCreateDroplet(createDropletOps)
			c <- drop
		}(i)
	}

	// wait for the droplets to be created in parallel
	for i := startingIdx; i < (startingIdx + createCount); i++ {
		select {
		case drop := <-c:
			droplets = append(droplets, *drop)
		case <-time.After(300 * time.Second):
			return errors.New("timeout creating droplet")
		}
	}
	return nil
}

// just take the current size diff and take that action.
func (m *Micropig) scaleDownSimple(asg *Asg) error {
	rmCount := asg.CurrentSize() - asg.Desired
	for i := 0; i < rmCount; i++ {
		dropID := asg.Droplets[i].ID
		m.MustDeleteDroplet(dropID)
	}
	return nil
}

func serializeTags(tags []string) string {
	return strings.Join(tags, ",")
}

func deserializeTags(tags string) []string {
	return strings.Split(tags, ",")
}

func (m *Micropig) updateAsg(asg *Asg) error {
	err := m.updateAsgDroplets(asg)
	if err != nil {
		return err
	}
	err = m.updateAsgStatus(asg)
	if err != nil {
		return err
	}
	return nil
}

func (m *Micropig) updateAsgDroplets(asg *Asg) error {
	listOpts := &godo.ListOptions{Page: 1, PerPage: 200}
	droplets, _, err := m.DoClient.Droplets.ListByTag(m.Ctx, asg.IDTag(), listOpts)
	if err != nil {
		return err
	}
	asg.Droplets = droplets
	return nil
}

func (m *Micropig) updateAsgStatus(asg *Asg) error {
	allActive := true
	currentSize := asg.CurrentSize()

	for _, drop := range asg.Droplets {
		status := drop.Status
		if status != "active" {
			allActive = false
			break
		}
	}

	if currentSize == asg.Desired {
		if currentSize == 0 {
			asg.Status = "empty"
		} else if allActive {
			asg.Status = "ok"
		} else {
			asg.Status = "not_active"
		}
	} else if currentSize < asg.Desired {
		asg.Status = "under"
	} else if currentSize > asg.Desired {
		asg.Status = "over"
	} else {
		return errors.New("unknown problem with asg")
	}

	return nil
}

func (m *Micropig) ListAsgs() ([]Asg, error) {
	var asgs []Asg
	err := m.Db.Find(&asgs).Error
	if err != nil {
		return nil, err
	}

	c := make(chan *Asg)
	for _, asg := range asgs {
		go func() {
			m.updateAsg(&asg)
			c <- &asg
		}()
	}

	updatedAsgs := make([]Asg, 0)

	for i := 0; i < len(asgs); i++ {
		select {
		case asg := <-c:
			updatedAsgs = append(updatedAsgs, *asg)
		case <-time.After(30 * time.Second):
			return nil, errors.New("timeout in ListAsgs")
		}
	}

	return updatedAsgs, nil
}

//??????????????????????????????????????????????????????????????????????????????????????????
//??????????????????????????????????????????????????????????????????????????????????????????
//??????????????????????????????????????????????????????????????????????????????????????????
//??????????????????????????????????????????????????????????????????????????????????????????
//??????????????????????????????????????????????????????????????????????????????????????????
//??????????????????????????????????????????????????????????????????????????????????????????
//??????????????????????????????????????????????????????????????????????????????????????????
//??????????????????????????????????????????????????????????????????????????????????????????
//??????????????????????????????????????????????????????????????????????????????????????????

//func (m *Micropig) listAsgsWithName(name *string) ([]Asg, error) {
//asgs := make([]Asg, 0)
//instanceMap := make(map[string][]godo.Droplet, 0)

//listOpts := &godo.ListOptions{Page: 1, PerPage: 200}
//droplets, _, err := m.DoClient.Droplets.List(m.Ctx, listOpts)
//if err != nil {
//return nil, err
//}

//for _, drop := range droplets {
//asgName := getTagValueS("asg", drop.Tags)
//if name == nil || (name != nil && asgName == *name) {
//if instanceMap[asgName] != nil {
//instanceMap[asgName] = append(instanceMap[asgName], drop)
//} else {
//instanceMap[asgName] = []godo.Droplet{drop}
//}
//}
//}

//for asgName, drops := range instanceMap {
//asg := &Asg{
//Name:     asgName,
//Droplets: drops,
////Desired:  getTagValueI("asg_desired", drops[0].Tags),
////Min:      getTagValueI("asg_min", drops[0].Tags),
////Max:      getTagValueI("asg_max", drops[0].Tags),
//}
//m.updateAsgStatus(asg, drops)
//asgs = append(asgs, *asg)
//}

//return asgs, nil
//}

//func getTagValueS(key string, tags []string) string {
//for _, tag := range tags {
//if strings.HasPrefix(tag, key) {
//parts := strings.SplitN(tag, ":", 2)
//return parts[1]
//}
//}
//panic(errors.New("couldnt find droplet tag"))
//}

//func getTagValueI(key string, tags []string) int {
//val := getTagValueS(key, tags)
//valI, err := strconv.ParseInt(val, 0, 64)
//if err != nil {
//panic(err)
//}
//return int(valI)
//}

//func (m *Micropig) getAsgByName(name string) (*Asg, error) {
//asgs, err := m.listAsgsWithName(&name)
//if err != nil {
//return nil, err
//}
//if len(asgs) == 0 {
//return nil, nil
//}
//return &asgs[0], nil
//}
