// -*- tab-width:2 -*-

package main

import (
	"github.com/lestrrat-go/file-rotatelogs"
	"log"
	"os/user"
	"sync/atomic"
	"time"
)

const (
	network = iota
	state
	all
	none
)

type lll struct {
	module string
	level  int
}

var initOnceDone int64 = 0

func initOnce() {
	if atomic.LoadInt64(&initOnceDone) == 1 {
		return
	}
	atomic.StoreInt64(&initOnceDone, 1)
	logPathTemplate := "/var/log/proxy.log.%Y%m%d"
	u, err := user.Current()
	if err != nil {
		log.Panic("Can't check user id")
	}
	if u.Uid != "0" {
		logPathTemplate = "./proxy.log.%Y%m%d"
	}
	// init rotating logs
	r1, err := rotatelogs.New(
		logPathTemplate,
		rotatelogs.WithMaxAge(time.Hour*168),
	)
	if err != nil {
		log.Panic("Can't open rotating logs")
	}
	log.SetOutput(r1)

}

func initLll(modName string, level string) lll {
	initOnce()
	if len(modName) > 50 {
		log.Panic("init_lll called with giant module name", modName)
	}
	var theLev int
	if level == "network" {
		theLev = network
	} else if level == "none" {
		theLev = none
	} else if level == "state" {
		theLev = state
	} else {
		theLev = all
	}
	res := lll{module: modName, level: theLev}
	return res
}

func (ll lll) ln(ls ...interface{}) {
	if ll.level > network {
		return
	}
	log.Println(ls...)
}

func (ll lll) ls(ls ...interface{}) {
	if ll.level > state {
		return
	}
	log.Println(ls...)
}

func (ll lll) la(ls ...interface{}) {
	if ll.level > all {
		return
	}
	log.Println(ls...)
}
