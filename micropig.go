package main

import (
	"context"

	"github.com/digitalocean/godo"
	"github.com/jinzhu/gorm"
)

type Micropig struct {
	DoClient *godo.Client
	Ctx      context.Context
	DryRun   bool
	Db       *gorm.DB
}
