# Copyright 2011 The Go Authors. All rights reserved.
# Use of this source code is governed by a BSD-style
# license that can be found in the LICENSE file.

include ../../Make.inc

TARG=gofix
GOFILES=\
	fix.go\
	netdial.go\
	main.go\
	osopen.go\
	httpfinalurl.go\
	httpserver.go\
	procattr.go\
	reflect.go\
	typecheck.go\

include ../../Make.cmd

test:
	gotest

testshort:
	gotest -test.short
