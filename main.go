package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strconv"

	"github.com/alexebird/tableme/tableme"
	"github.com/davecgh/go-spew/spew"
	"github.com/digitalocean/godo"
	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/postgres"
	"github.com/urfave/cli"
	"golang.org/x/oauth2"
)

var s = spew.Dump

type TokenSource struct {
	AccessToken string
}

func (t *TokenSource) Token() (*oauth2.Token, error) {
	token := &oauth2.Token{
		AccessToken: t.AccessToken,
	}
	return token, nil
}

func cliDbAutomigrateAction(c *cli.Context, m *Micropig) error {
	m.Db.AutoMigrate(&Asg{})
	return nil
}

func cliAsgCreateAction(c *cli.Context, m *Micropig) error {
	name := c.String("name")
	fmt.Println("creating asg name=" + name)

	asgOpts := &CreateAsgOptions{
		Name:     name,
		Region:   c.String("region"),
		Size:     c.String("size"),
		Snapshot: c.String("snapshot"),
		Tags:     c.StringSlice("tags"),
		Desired:  c.Int("desired"),
		Min:      c.Int("min"),
		Max:      c.Int("max"),
	}

	asg, err := m.CreateAsg(asgOpts)
	if err != nil {
		return err
	}

	m.ScaleToDesired(asg)
	fmt.Println("created asg name=" + asg.Name)

	if !m.DryRun && c.Bool("wait") {
		fmt.Println("waiting for asg name=" + asg.Name)
		m.WaitForAsgStatus(asg, "ok")
	}
	fmt.Println("asg ok name=" + asg.Name)

	return nil
}

func cliAsgLsAction(c *cli.Context, m *Micropig) error {
	asgs, err := m.ListAsgs()
	if err != nil {
		return err
	}

	var records [][]string
	for _, asg := range asgs {
		records = append(records, []string{
			asg.Name,
			strconv.Itoa(asg.CurrentSize()),
			asg.Region,
			asg.Size,
			asg.TagsStr,
			asg.ImageSlug,
		})
	}
	bites := tableme.TableMe([]string{"NAME", "CURR", "REGION", "SIZE", "TAGS", "IMAGE"}, records)
	buff := bytes.NewBuffer(bites)
	fmt.Print(buff.String())

	return nil
}

func cliAsgDeleteAction(c *cli.Context, m *Micropig) error {
	name := c.String("name")
	err := m.DeleteAsgByName(name)
	if err != nil {
		return err
	}
	fmt.Println("removed asg name=" + name)
	return nil
}

func main() {
	ctx := context.TODO()
	db, err := gorm.Open("postgres",
		fmt.Sprintf("host=%s port=%s user=%s dbname=%s password=%s",
			"localhost", "5432", "micropig", "micropig", "MyPassword",
		),
	)
	defer db.Close()

	tokenSource := &TokenSource{AccessToken: os.Getenv("DIGITALOCEAN_TOKEN")}
	m := &Micropig{
		DryRun:   false,
		Ctx:      ctx,
		DoClient: godo.NewClient(oauth2.NewClient(ctx, tokenSource)),
		Db:       db,
	}

	app := cli.NewApp()
	app.Name = "micropig"
	app.Usage = "oink"
	app.Flags = []cli.Flag{
		cli.BoolFlag{Name: "dry-run"},
	}

	app.Commands = []cli.Command{
		{
			Name:  "db",
			Usage: "db operations",
			Subcommands: []cli.Command{
				{
					Name: "automigrate",
					Action: func(c *cli.Context) error {
						return cliDbAutomigrateAction(c, m)
					},
				},
			},
		},
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
						cli.StringSliceFlag{Name: "tags, T"},
						cli.IntFlag{Name: "max"},
						cli.IntFlag{Name: "min"},
						cli.IntFlag{Name: "desired"},
						cli.BoolFlag{Name: "wait"},
					},
					Action: func(c *cli.Context) error {
						if c.GlobalBool("dry-run") {
							m.DryRun = true
							fmt.Println("DRY RUN!!!")
						}
						return cliAsgCreateAction(c, m)
					},
				},
				{
					Name:  "ls",
					Usage: "list asgs",
					Action: func(c *cli.Context) error {
						return cliAsgLsAction(c, m)
					},
				},
				{
					Name:  "rm",
					Usage: "rm asg",
					Flags: []cli.Flag{
						cli.StringFlag{Name: "name, n"},
					},
					Action: func(c *cli.Context) error {
						if c.GlobalBool("dry-run") {
							m.DryRun = true
							fmt.Println("DRY RUN!!!")
						}
						return cliAsgDeleteAction(c, m)
					},
				},
			},
		},
	}

	err = app.Run(os.Args)
	if err != nil {
		panic(err)
	}
}
