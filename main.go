package main

import (
	//"bytes"
	"context"
	"fmt"
	"os"

	//"github.com/alexebird/tableme/tableme"
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

	asgOpts := &createAsgOptions{
		name:     name,
		region:   c.String("region"),
		size:     c.String("size"),
		snapshot: c.String("snapshot"),
		desired:  c.Int("desired"),
		min:      c.Int("min"),
		max:      c.Int("max"),
	}

	asg, err := m.createAsg(asgOpts)
	if err != nil {
		panic(err)
	}

	fmt.Println("created asg name=" + asg.name)
	//s(asg)
	fmt.Println("waiting for asg name=" + asg.name)
	m.waitForAsgStatus(asg, "ok")
	fmt.Println("asg ok name=" + asg.name)
	//s(asg)
	return nil
}

func main() {
	tokenSource := &TokenSource{AccessToken: os.Getenv("DIGITALOCEAN_TOKEN")}
	ctx := context.TODO()
	db, err := gorm.Open("postgres",
		fmt.Sprintf("host=%s port=%s user=%s dbname=%s password=%s",
			"localhost", "5432", "micropig", "micropig", "MyPassword",
		),
	)
	defer db.Close()

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
						cli.StringFlag{Name: "type, t"},
						cli.BoolFlag{Name: "wait"},
						cli.IntFlag{Name: "max"},
						cli.IntFlag{Name: "min"},
						cli.IntFlag{Name: "desired"},
					},
					Action: cliAsgCreateAction,
				},
				//{
				//Name:  "ls",
				//Usage: "list asgs",
				//Action: func(c *cli.Context) error {
				//asgs, err := d.listAsgs()
				//if err != nil {
				//panic(err)
				//}

				//var records [][]string
				//for _, asg := range asgs {
				//records = append(records, []string{
				//asg.name,
				//strconv.Itoa(len(asg.droplets)),
				//})
				//}
				//bites := tableme.TableMe([]string{"NAME", "CURR"}, records)
				//buff := bytes.NewBuffer(bites)
				//fmt.Print(buff.String())

				////s(asgs)
				//return nil
				//},
				//},
				//{
				//Name:  "rm",
				//Usage: "rm asg",
				//Flags: []cli.Flag{
				//cli.StringFlag{Name: "name, n"},
				//},
				//Action: func(c *cli.Context) error {
				//name := c.String("name")
				//err := d.rmAsg(name)
				//if err != nil {
				//panic(err)
				//}

				//return nil
				//},
				//},
			},
		},
	}

	err = app.Run(os.Args)
	if err != nil {
		panic(err)
	}
}
