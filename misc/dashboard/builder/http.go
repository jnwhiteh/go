// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"fmt"
	"http"
	"json"
	"log"
	"os"
	"strconv"
)

type param map[string]string

// dash runs the given method and command on the dashboard.
// If args is not nil, it is the query or post parameters.
// If resp is not nil, dash unmarshals the body as JSON into resp.
func dash(meth, cmd string, resp interface{}, args param) os.Error {
	var r *http.Response
	var err os.Error
	if *verbose {
		log.Println("dash", cmd, args)
	}
	cmd = "http://" + *dashboard + "/" + cmd
	vals := make(http.Values)
	for k, v := range args {
		vals.Add(k, v)
	}
	switch meth {
	case "GET":
		if q := vals.Encode(); q != "" {
			cmd += "?" + q
		}
		r, err = http.Get(cmd)
	case "POST":
		r, err = http.PostForm(cmd, vals)
	default:
		return fmt.Errorf("unknown method %q", meth)
	}
	if err != nil {
		return err
	}
	defer r.Body.Close()
	var buf bytes.Buffer
	buf.ReadFrom(r.Body)
	if resp != nil {
		if err = json.Unmarshal(buf.Bytes(), resp); err != nil {
			log.Printf("json unmarshal %#q: %s\n", buf.Bytes(), err)
			return err
		}
	}
	return nil
}

func dashStatus(meth, cmd string, args param) os.Error {
	var resp struct {
		Status string
		Error  string
	}
	err := dash(meth, cmd, &resp, args)
	if err != nil {
		return err
	}
	if resp.Status != "OK" {
		return os.NewError("/build: " + resp.Error)
	}
	return nil
}

// todo returns the next hash to build.
func (b *Builder) todo() (rev string, err os.Error) {
	var resp []struct {
		Hash string
	}
	if err = dash("GET", "todo", &resp, param{"builder": b.name}); err != nil {
		return
	}
	if len(resp) > 0 {
		rev = resp[0].Hash
	}
	return
}

// recordResult sends build results to the dashboard
func (b *Builder) recordResult(buildLog string, hash string) os.Error {
	return dash("POST", "build", nil, param{
		"builder": b.name,
		"key":     b.key,
		"node":    hash,
		"log":     buildLog,
	})
}

// packages fetches a list of package paths from the dashboard
func packages() (pkgs []string, err os.Error) {
	var resp struct {
		Packages []struct {
			Path string
		}
	}
	err = dash("GET", "package", &resp, param{"fmt": "json"})
	if err != nil {
		return
	}
	for _, p := range resp.Packages {
		pkgs = append(pkgs, p.Path)
	}
	return
}

// updatePackage sends package build results and info to the dashboard
func (b *Builder) updatePackage(pkg string, state bool, buildLog, info string, hash string) os.Error {
	return dash("POST", "package", nil, param{
		"builder": b.name,
		"key":     b.key,
		"path":    pkg,
		"state":   strconv.Btoa(state),
		"log":     buildLog,
		"info":    info,
		"go_rev":  hash[:12],
	})
}

// postCommit informs the dashboard of a new commit
func postCommit(key string, l *HgLog) os.Error {
	return dashStatus("POST", "commit", param{
		"key":    key,
		"node":   l.Hash,
		"date":   l.Date,
		"user":   l.Author,
		"parent": l.Parent,
		"desc":   l.Desc,
	})
}

// dashboardCommit returns true if the dashboard knows about hash.
func dashboardCommit(hash string) bool {
	err := dashStatus("GET", "commit", param{"node": hash})
	if err != nil {
		log.Printf("check %s: %s", hash, err)
		return false
	}
	return true
}
