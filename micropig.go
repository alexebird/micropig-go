package main

import (
	//"bytes"
	"context"

	//"github.com/alexebird/tableme/tableme"

	"github.com/digitalocean/godo"
	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/postgres"
)

type Micropig struct {
	DoClient *godo.Client
	Ctx      context.Context
	DryRun   bool
	Db       *gorm.DB
}
