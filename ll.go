// -*- tab-width:2 -*-

package main

import (
	"log"
)

const (
	debug = iota
	regular
	none
)

type lll struct {
	module string
	level  int
}

func initLll(modName string, level string) lll {
	if len(modName) > 50 {
		log.Panic("init_lll called with giant module name", modName)
	}
	var theLev int
	if level == "debug" {
		theLev = debug
	} else if level == "none" {
		theLev = none
	} else {
		theLev = regular
	}
	res := lll{module: modName, level: theLev}
	return res
}

func (ll lll) ld(ls ...interface{}) {
	if ll.level != debug {
		return
	}
	log.Println(ls...)
}

func (ll lll) la(ls ...interface{}) {
	if ll.level == none {
		return
	}
	log.Println(ls...)
}
