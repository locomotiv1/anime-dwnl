package main

import (
	"github.com/nstratos/go-myanimelist/mal"
)

type demoClient struct {
	*mal.Client
	err error
}
