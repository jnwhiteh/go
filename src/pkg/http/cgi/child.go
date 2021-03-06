// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// This file implements CGI from the perspective of a child
// process.

package cgi

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"http"
	"io"
	"io/ioutil"
	"net"
	"os"
	"strconv"
	"strings"
)

// Request returns the HTTP request as represented in the current
// environment. This assumes the current program is being run
// by a web server in a CGI environment.
// The returned Request's Body is populated, if applicable.
func Request() (*http.Request, os.Error) {
	r, err := RequestFromMap(envMap(os.Environ()))
	if err != nil {
		return nil, err
	}
	if r.ContentLength > 0 {
		r.Body = ioutil.NopCloser(io.LimitReader(os.Stdin, r.ContentLength))
	}
	return r, nil
}

func envMap(env []string) map[string]string {
	m := make(map[string]string)
	for _, kv := range env {
		if idx := strings.Index(kv, "="); idx != -1 {
			m[kv[:idx]] = kv[idx+1:]
		}
	}
	return m
}

// These environment variables are manually copied into Request
var skipHeader = map[string]bool{
	"HTTP_HOST":       true,
	"HTTP_REFERER":    true,
	"HTTP_USER_AGENT": true,
}

// RequestFromMap creates an http.Request from CGI variables.
// The returned Request's Body field is not populated.
func RequestFromMap(params map[string]string) (*http.Request, os.Error) {
	r := new(http.Request)
	r.Method = params["REQUEST_METHOD"]
	if r.Method == "" {
		return nil, os.NewError("cgi: no REQUEST_METHOD in environment")
	}

	r.Proto = params["SERVER_PROTOCOL"]
	var ok bool
	r.ProtoMajor, r.ProtoMinor, ok = http.ParseHTTPVersion(r.Proto)
	if !ok {
		return nil, os.NewError("cgi: invalid SERVER_PROTOCOL version")
	}

	r.Close = true
	r.Trailer = http.Header{}
	r.Header = http.Header{}

	r.Host = params["HTTP_HOST"]
	r.Referer = params["HTTP_REFERER"]
	r.UserAgent = params["HTTP_USER_AGENT"]

	if lenstr := params["CONTENT_LENGTH"]; lenstr != "" {
		clen, err := strconv.Atoi64(lenstr)
		if err != nil {
			return nil, os.NewError("cgi: bad CONTENT_LENGTH in environment: " + lenstr)
		}
		r.ContentLength = clen
	}

	if ct := params["CONTENT_TYPE"]; ct != "" {
		r.Header.Set("Content-Type", ct)
	}

	// Copy "HTTP_FOO_BAR" variables to "Foo-Bar" Headers
	for k, v := range params {
		if !strings.HasPrefix(k, "HTTP_") || skipHeader[k] {
			continue
		}
		r.Header.Add(strings.Replace(k[5:], "_", "-", -1), v)
	}

	// TODO: cookies.  parsing them isn't exported, though.

	if r.Host != "" {
		// Hostname is provided, so we can reasonably construct a URL,
		// even if we have to assume 'http' for the scheme.
		r.RawURL = "http://" + r.Host + params["REQUEST_URI"]
		url, err := http.ParseURL(r.RawURL)
		if err != nil {
			return nil, os.NewError("cgi: failed to parse host and REQUEST_URI into a URL: " + r.RawURL)
		}
		r.URL = url
	}
	// Fallback logic if we don't have a Host header or the URL
	// failed to parse
	if r.URL == nil {
		r.RawURL = params["REQUEST_URI"]
		url, err := http.ParseURL(r.RawURL)
		if err != nil {
			return nil, os.NewError("cgi: failed to parse REQUEST_URI into a URL: " + r.RawURL)
		}
		r.URL = url
	}

	// There's apparently a de-facto standard for this.
	// http://docstore.mik.ua/orelly/linux/cgi/ch03_02.htm#ch03-35636
	if s := params["HTTPS"]; s == "on" || s == "ON" || s == "1" {
		r.TLS = &tls.ConnectionState{HandshakeComplete: true}
	}

	// Request.RemoteAddr has its port set by Go's standard http
	// server, so we do here too. We don't have one, though, so we
	// use a dummy one.
	r.RemoteAddr = net.JoinHostPort(params["REMOTE_ADDR"], "0")

	return r, nil
}

// Serve executes the provided Handler on the currently active CGI
// request, if any. If there's no current CGI environment
// an error is returned. The provided handler may be nil to use
// http.DefaultServeMux.
func Serve(handler http.Handler) os.Error {
	req, err := Request()
	if err != nil {
		return err
	}
	if handler == nil {
		handler = http.DefaultServeMux
	}
	rw := &response{
		req:    req,
		header: make(http.Header),
		bufw:   bufio.NewWriter(os.Stdout),
	}
	handler.ServeHTTP(rw, req)
	if err = rw.bufw.Flush(); err != nil {
		return err
	}
	return nil
}

type response struct {
	req        *http.Request
	header     http.Header
	bufw       *bufio.Writer
	headerSent bool
}

func (r *response) Flush() {
	r.bufw.Flush()
}

func (r *response) Header() http.Header {
	return r.header
}

func (r *response) Write(p []byte) (n int, err os.Error) {
	if !r.headerSent {
		r.WriteHeader(http.StatusOK)
	}
	return r.bufw.Write(p)
}

func (r *response) WriteHeader(code int) {
	if r.headerSent {
		// Note: explicitly using Stderr, as Stdout is our HTTP output.
		fmt.Fprintf(os.Stderr, "CGI attempted to write header twice on request for %s", r.req.URL)
		return
	}
	r.headerSent = true
	fmt.Fprintf(r.bufw, "Status: %d %s\r\n", code, http.StatusText(code))

	// Set a default Content-Type
	if _, hasType := r.header["Content-Type"]; !hasType {
		r.header.Add("Content-Type", "text/html; charset=utf-8")
	}

	r.header.Write(r.bufw)
	r.bufw.WriteString("\r\n")
	r.bufw.Flush()
}
