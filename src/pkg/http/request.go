// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// HTTP Request reading and parsing.

// Package http implements parsing of HTTP requests, replies, and URLs and
// provides an extensible HTTP server and a basic HTTP client.
package http

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"container/vector"
	"encoding/base64"
	"fmt"
	"io"
	"io/ioutil"
	"mime"
	"mime/multipart"
	"net/textproto"
	"os"
	"strconv"
	"strings"
)

const (
	maxLineLength    = 4096 // assumed <= bufio.defaultBufSize
	maxValueLength   = 4096
	maxHeaderLines   = 1024
	chunkSize        = 4 << 10  // 4 KB chunks
	defaultMaxMemory = 32 << 20 // 32 MB
)

// ErrMissingFile is returned by FormFile when the provided file field name
// is either not present in the request or not a file field.
var ErrMissingFile = os.ErrorString("http: no such file")

// HTTP request parsing errors.
type ProtocolError struct {
	os.ErrorString
}

var (
	ErrLineTooLong          = &ProtocolError{"header line too long"}
	ErrHeaderTooLong        = &ProtocolError{"header too long"}
	ErrShortBody            = &ProtocolError{"entity body too short"}
	ErrNotSupported         = &ProtocolError{"feature not supported"}
	ErrUnexpectedTrailer    = &ProtocolError{"trailer header without chunked transfer encoding"}
	ErrMissingContentLength = &ProtocolError{"missing ContentLength in HEAD response"}
	ErrNotMultipart         = &ProtocolError{"request Content-Type isn't multipart/form-data"}
	ErrMissingBoundary      = &ProtocolError{"no multipart boundary param Content-Type"}
)

type badStringError struct {
	what string
	str  string
}

func (e *badStringError) String() string { return fmt.Sprintf("%s %q", e.what, e.str) }

var reqExcludeHeader = map[string]bool{
	"Host":              true,
	"User-Agent":        true,
	"Referer":           true,
	"Content-Length":    true,
	"Transfer-Encoding": true,
	"Trailer":           true,
}

// A Request represents a parsed HTTP request header.
type Request struct {
	Method string // GET, POST, PUT, etc.
	RawURL string // The raw URL given in the request.
	URL    *URL   // Parsed URL.

	// The protocol version for incoming requests.
	// Outgoing requests always use HTTP/1.1.
	Proto      string // "HTTP/1.0"
	ProtoMajor int    // 1
	ProtoMinor int    // 0

	// A header maps request lines to their values.
	// If the header says
	//
	//	accept-encoding: gzip, deflate
	//	Accept-Language: en-us
	//	Connection: keep-alive
	//
	// then
	//
	//	Header = map[string][]string{
	//		"Accept-Encoding": {"gzip, deflate"},
	//		"Accept-Language": {"en-us"},
	//		"Connection": {"keep-alive"},
	//	}
	//
	// HTTP defines that header names are case-insensitive.
	// The request parser implements this by canonicalizing the
	// name, making the first character and any characters
	// following a hyphen uppercase and the rest lowercase.
	Header Header

	// Cookie records the HTTP cookies sent with the request.
	Cookie []*Cookie

	// The message body.
	Body io.ReadCloser

	// ContentLength records the length of the associated content.
	// The value -1 indicates that the length is unknown.
	// Values >= 0 indicate that the given number of bytes may be read from Body.
	ContentLength int64

	// TransferEncoding lists the transfer encodings from outermost to innermost.
	// An empty list denotes the "identity" encoding.
	TransferEncoding []string

	// Whether to close the connection after replying to this request.
	Close bool

	// The host on which the URL is sought.
	// Per RFC 2616, this is either the value of the Host: header
	// or the host name given in the URL itself.
	Host string

	// The referring URL, if sent in the request.
	//
	// Referer is misspelled as in the request itself,
	// a mistake from the earliest days of HTTP.
	// This value can also be fetched from the Header map
	// as Header["Referer"]; the benefit of making it
	// available as a structure field is that the compiler
	// can diagnose programs that use the alternate
	// (correct English) spelling req.Referrer but cannot
	// diagnose programs that use Header["Referrer"].
	Referer string

	// The User-Agent: header string, if sent in the request.
	UserAgent string

	// The parsed form. Only available after ParseForm is called.
	Form Values

	// The parsed multipart form, including file uploads.
	// Only available after ParseMultipartForm is called.
	MultipartForm *multipart.Form

	// Trailer maps trailer keys to values.  Like for Header, if the
	// response has multiple trailer lines with the same key, they will be
	// concatenated, delimited by commas.
	Trailer Header

	// RemoteAddr allows HTTP servers and other software to record
	// the network address that sent the request, usually for
	// logging. This field is not filled in by ReadRequest and
	// has no defined format. The HTTP server in this package
	// sets RemoteAddr to an "IP:port" address before invoking a
	// handler.
	RemoteAddr string

	// TLS allows HTTP servers and other software to record
	// information about the TLS connection on which the request
	// was received. This field is not filled in by ReadRequest.
	// The HTTP server in this package sets the field for
	// TLS-enabled connections before invoking a handler;
	// otherwise it leaves the field nil.
	TLS *tls.ConnectionState
}

// ProtoAtLeast returns whether the HTTP protocol used
// in the request is at least major.minor.
func (r *Request) ProtoAtLeast(major, minor int) bool {
	return r.ProtoMajor > major ||
		r.ProtoMajor == major && r.ProtoMinor >= minor
}

// multipartByReader is a sentinel value.
// Its presence in Request.MultipartForm indicates that parsing of the request
// body has been handed off to a MultipartReader instead of ParseMultipartFrom.
var multipartByReader = &multipart.Form{
	Value: make(map[string][]string),
	File:  make(map[string][]*multipart.FileHeader),
}

// MultipartReader returns a MIME multipart reader if this is a
// multipart/form-data POST request, else returns nil and an error.
// Use this function instead of ParseMultipartForm to
// process the request body as a stream.
func (r *Request) MultipartReader() (multipart.Reader, os.Error) {
	if r.MultipartForm == multipartByReader {
		return nil, os.NewError("http: MultipartReader called twice")
	}
	if r.MultipartForm != nil {
		return nil, os.NewError("http: multipart handled by ParseMultipartForm")
	}
	r.MultipartForm = multipartByReader
	return r.multipartReader()
}

func (r *Request) multipartReader() (multipart.Reader, os.Error) {
	v := r.Header.Get("Content-Type")
	if v == "" {
		return nil, ErrNotMultipart
	}
	d, params := mime.ParseMediaType(v)
	if d != "multipart/form-data" {
		return nil, ErrNotMultipart
	}
	boundary, ok := params["boundary"]
	if !ok {
		return nil, ErrMissingBoundary
	}
	return multipart.NewReader(r.Body, boundary), nil
}

// Return value if nonempty, def otherwise.
func valueOrDefault(value, def string) string {
	if value != "" {
		return value
	}
	return def
}

const defaultUserAgent = "Go http package"

// Write writes an HTTP/1.1 request -- header and body -- in wire format.
// This method consults the following fields of req:
//	Host
//	RawURL, if non-empty, or else URL
//	Method (defaults to "GET")
//	UserAgent (defaults to defaultUserAgent)
//	Referer
//	Header (only keys not already in this list)
//	Cookie
//	ContentLength
//	TransferEncoding
//	Body
//
// If Body is present, Content-Length is <= 0 and TransferEncoding
// hasn't been set to "identity", Write adds "Transfer-Encoding:
// chunked" to the header. Body is closed after it is sent.
func (req *Request) Write(w io.Writer) os.Error {
	return req.write(w, false)
}

// WriteProxy is like Write but writes the request in the form
// expected by an HTTP proxy.  It includes the scheme and host
// name in the URI instead of using a separate Host: header line.
// If req.RawURL is non-empty, WriteProxy uses it unchanged
// instead of URL but still omits the Host: header.
func (req *Request) WriteProxy(w io.Writer) os.Error {
	return req.write(w, true)
}

func (req *Request) write(w io.Writer, usingProxy bool) os.Error {
	host := req.Host
	if host == "" {
		if req.URL == nil {
			return os.NewError("http: Request.Write on Request with no Host or URL set")
		}
		host = req.URL.Host
	}

	uri := req.RawURL
	if uri == "" {
		uri = valueOrDefault(urlEscape(req.URL.Path, encodePath), "/")
		if req.URL.RawQuery != "" {
			uri += "?" + req.URL.RawQuery
		}
		if usingProxy {
			if uri == "" || uri[0] != '/' {
				uri = "/" + uri
			}
			uri = req.URL.Scheme + "://" + host + uri
		}
	}

	fmt.Fprintf(w, "%s %s HTTP/1.1\r\n", valueOrDefault(req.Method, "GET"), uri)

	// Header lines
	fmt.Fprintf(w, "Host: %s\r\n", host)
	fmt.Fprintf(w, "User-Agent: %s\r\n", valueOrDefault(req.UserAgent, defaultUserAgent))
	if req.Referer != "" {
		fmt.Fprintf(w, "Referer: %s\r\n", req.Referer)
	}

	// Process Body,ContentLength,Close,Trailer
	tw, err := newTransferWriter(req)
	if err != nil {
		return err
	}
	err = tw.WriteHeader(w)
	if err != nil {
		return err
	}

	// TODO: split long values?  (If so, should share code with Conn.Write)
	// TODO: if Header includes values for Host, User-Agent, or Referer, this
	// may conflict with the User-Agent or Referer headers we add manually.
	// One solution would be to remove the Host, UserAgent, and Referer fields
	// from Request, and introduce Request methods along the lines of
	// Response.{GetHeader,AddHeader} and string constants for "Host",
	// "User-Agent" and "Referer".
	err = req.Header.WriteSubset(w, reqExcludeHeader)
	if err != nil {
		return err
	}

	if err = writeCookies(w, req.Cookie); err != nil {
		return err
	}

	io.WriteString(w, "\r\n")

	// Write body and trailer
	err = tw.WriteBody(w)
	if err != nil {
		return err
	}

	return nil
}

// Read a line of bytes (up to \n) from b.
// Give up if the line exceeds maxLineLength.
// The returned bytes are a pointer into storage in
// the bufio, so they are only valid until the next bufio read.
func readLineBytes(b *bufio.Reader) (p []byte, err os.Error) {
	if p, err = b.ReadSlice('\n'); err != nil {
		// We always know when EOF is coming.
		// If the caller asked for a line, there should be a line.
		if err == os.EOF {
			err = io.ErrUnexpectedEOF
		} else if err == bufio.ErrBufferFull {
			err = ErrLineTooLong
		}
		return nil, err
	}
	if len(p) >= maxLineLength {
		return nil, ErrLineTooLong
	}

	// Chop off trailing white space.
	var i int
	for i = len(p); i > 0; i-- {
		if c := p[i-1]; c != ' ' && c != '\r' && c != '\t' && c != '\n' {
			break
		}
	}
	return p[0:i], nil
}

// readLineBytes, but convert the bytes into a string.
func readLine(b *bufio.Reader) (s string, err os.Error) {
	p, e := readLineBytes(b)
	if e != nil {
		return "", e
	}
	return string(p), nil
}

// Convert decimal at s[i:len(s)] to integer,
// returning value, string position where the digits stopped,
// and whether there was a valid number (digits, not too big).
func atoi(s string, i int) (n, i1 int, ok bool) {
	const Big = 1000000
	if i >= len(s) || s[i] < '0' || s[i] > '9' {
		return 0, 0, false
	}
	n = 0
	for ; i < len(s) && '0' <= s[i] && s[i] <= '9'; i++ {
		n = n*10 + int(s[i]-'0')
		if n > Big {
			return 0, 0, false
		}
	}
	return n, i, true
}

// ParseHTTPVersion parses a HTTP version string.
// "HTTP/1.0" returns (1, 0, true).
func ParseHTTPVersion(vers string) (major, minor int, ok bool) {
	if len(vers) < 5 || vers[0:5] != "HTTP/" {
		return 0, 0, false
	}
	major, i, ok := atoi(vers, 5)
	if !ok || i >= len(vers) || vers[i] != '.' {
		return 0, 0, false
	}
	minor, i, ok = atoi(vers, i+1)
	if !ok || i != len(vers) {
		return 0, 0, false
	}
	return major, minor, true
}

type chunkedReader struct {
	r   *bufio.Reader
	n   uint64 // unread bytes in chunk
	err os.Error
}

func newChunkedReader(r *bufio.Reader) *chunkedReader {
	return &chunkedReader{r: r}
}

func (cr *chunkedReader) beginChunk() {
	// chunk-size CRLF
	var line string
	line, cr.err = readLine(cr.r)
	if cr.err != nil {
		return
	}
	cr.n, cr.err = strconv.Btoui64(line, 16)
	if cr.err != nil {
		return
	}
	if cr.n == 0 {
		// trailer CRLF
		for {
			line, cr.err = readLine(cr.r)
			if cr.err != nil {
				return
			}
			if line == "" {
				break
			}
		}
		cr.err = os.EOF
	}
}

func (cr *chunkedReader) Read(b []uint8) (n int, err os.Error) {
	if cr.err != nil {
		return 0, cr.err
	}
	if cr.n == 0 {
		cr.beginChunk()
		if cr.err != nil {
			return 0, cr.err
		}
	}
	if uint64(len(b)) > cr.n {
		b = b[0:cr.n]
	}
	n, cr.err = cr.r.Read(b)
	cr.n -= uint64(n)
	if cr.n == 0 && cr.err == nil {
		// end of chunk (CRLF)
		b := make([]byte, 2)
		if _, cr.err = io.ReadFull(cr.r, b); cr.err == nil {
			if b[0] != '\r' || b[1] != '\n' {
				cr.err = os.NewError("malformed chunked encoding")
			}
		}
	}
	return n, cr.err
}

// NewRequest returns a new Request given a method, URL, and optional body.
func NewRequest(method, url string, body io.Reader) (*Request, os.Error) {
	u, err := ParseURL(url)
	if err != nil {
		return nil, err
	}
	rc, ok := body.(io.ReadCloser)
	if !ok && body != nil {
		rc = ioutil.NopCloser(body)
	}
	req := &Request{
		Method:     method,
		URL:        u,
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header:     make(Header),
		Body:       rc,
		Host:       u.Host,
	}
	if body != nil {
		switch v := body.(type) {
		case *strings.Reader:
			req.ContentLength = int64(v.Len())
		case *bytes.Buffer:
			req.ContentLength = int64(v.Len())
		default:
			req.ContentLength = -1 // chunked
		}
		if req.ContentLength == 0 {
			// To prevent chunking and disambiguate this
			// from the default ContentLength zero value.
			req.TransferEncoding = []string{"identity"}
		}
	}

	return req, nil
}

// SetBasicAuth sets the request's Authorization header to use HTTP
// Basic Authentication with the provided username and password.
//
// With HTTP Basic Authentication the provided username and password
// are not encrypted.
func (r *Request) SetBasicAuth(username, password string) {
	s := username + ":" + password
	r.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(s)))
}

// ReadRequest reads and parses a request from b.
func ReadRequest(b *bufio.Reader) (req *Request, err os.Error) {

	tp := textproto.NewReader(b)
	req = new(Request)

	// First line: GET /index.html HTTP/1.0
	var s string
	if s, err = tp.ReadLine(); err != nil {
		if err == os.EOF {
			err = io.ErrUnexpectedEOF
		}
		return nil, err
	}

	var f []string
	if f = strings.Split(s, " ", 3); len(f) < 3 {
		return nil, &badStringError{"malformed HTTP request", s}
	}
	req.Method, req.RawURL, req.Proto = f[0], f[1], f[2]
	var ok bool
	if req.ProtoMajor, req.ProtoMinor, ok = ParseHTTPVersion(req.Proto); !ok {
		return nil, &badStringError{"malformed HTTP version", req.Proto}
	}

	if req.URL, err = ParseRequestURL(req.RawURL); err != nil {
		return nil, err
	}

	// Subsequent lines: Key: value.
	mimeHeader, err := tp.ReadMIMEHeader()
	if err != nil {
		return nil, err
	}
	req.Header = Header(mimeHeader)

	// RFC2616: Must treat
	//	GET /index.html HTTP/1.1
	//	Host: www.google.com
	// and
	//	GET http://www.google.com/index.html HTTP/1.1
	//	Host: doesntmatter
	// the same.  In the second case, any Host line is ignored.
	req.Host = req.URL.Host
	if req.Host == "" {
		req.Host = req.Header.Get("Host")
	}
	req.Header.Del("Host")

	fixPragmaCacheControl(req.Header)

	// Pull out useful fields as a convenience to clients.
	req.Referer = req.Header.Get("Referer")
	req.Header.Del("Referer")

	req.UserAgent = req.Header.Get("User-Agent")
	req.Header.Del("User-Agent")

	// TODO: Parse specific header values:
	//	Accept
	//	Accept-Encoding
	//	Accept-Language
	//	Authorization
	//	Cache-Control
	//	Connection
	//	Date
	//	Expect
	//	From
	//	If-Match
	//	If-Modified-Since
	//	If-None-Match
	//	If-Range
	//	If-Unmodified-Since
	//	Max-Forwards
	//	Proxy-Authorization
	//	Referer [sic]
	//	TE (transfer-codings)
	//	Trailer
	//	Transfer-Encoding
	//	Upgrade
	//	User-Agent
	//	Via
	//	Warning

	err = readTransfer(req, b)
	if err != nil {
		return nil, err
	}

	req.Cookie = readCookies(req.Header)

	return req, nil
}

// Values maps a string key to a list of values.
// It is typically used for query parameters and form values.
// Unlike in the Header map, the keys in a Values map
// are case-sensitive.
type Values map[string][]string

// Get gets the first value associated with the given key.
// If there are no values associated with the key, Get returns
// the empty string. To access multiple values, use the map
// directly.
func (v Values) Get(key string) string {
	if v == nil {
		return ""
	}
	vs, ok := v[key]
	if !ok || len(vs) == 0 {
		return ""
	}
	return vs[0]
}

// Set sets the key to value. It replaces any existing
// values.
func (v Values) Set(key, value string) {
	v[key] = []string{value}
}

// Add adds the key to value. It appends to any existing
// values associated with key.
func (v Values) Add(key, value string) {
	v[key] = append(v[key], value)
}

// Del deletes the values associated with key.
func (v Values) Del(key string) {
	v[key] = nil, false
}

// ParseQuery parses the URL-encoded query string and returns
// a map listing the values specified for each key.
// ParseQuery always returns a non-nil map containing all the
// valid query parameters found; err describes the first decoding error
// encountered, if any.
func ParseQuery(query string) (m Values, err os.Error) {
	m = make(Values)
	err = parseQuery(m, query)
	return
}

func parseQuery(m Values, query string) (err os.Error) {
	for _, kv := range strings.Split(query, "&", -1) {
		if len(kv) == 0 {
			continue
		}
		kvPair := strings.Split(kv, "=", 2)

		var key, value string
		var e os.Error
		key, e = URLUnescape(kvPair[0])
		if e == nil && len(kvPair) > 1 {
			value, e = URLUnescape(kvPair[1])
		}
		if e != nil {
			err = e
			continue
		}
		vec := vector.StringVector(m[key])
		vec.Push(value)
		m[key] = vec
	}
	return err
}

// ParseForm parses the raw query.
// For POST requests, it also parses the request body as a form.
// ParseMultipartForm calls ParseForm automatically.
// It is idempotent.
func (r *Request) ParseForm() (err os.Error) {
	if r.Form != nil {
		return
	}

	r.Form = make(Values)
	if r.URL != nil {
		err = parseQuery(r.Form, r.URL.RawQuery)
	}
	if r.Method == "POST" {
		if r.Body == nil {
			return os.ErrorString("missing form body")
		}
		ct := r.Header.Get("Content-Type")
		switch strings.Split(ct, ";", 2)[0] {
		case "text/plain", "application/x-www-form-urlencoded", "":
			const maxFormSize = int64(10 << 20) // 10 MB is a lot of text.
			b, e := ioutil.ReadAll(io.LimitReader(r.Body, maxFormSize+1))
			if e != nil {
				if err == nil {
					err = e
				}
				break
			}
			if int64(len(b)) > maxFormSize {
				return os.NewError("http: POST too large")
			}
			e = parseQuery(r.Form, string(b))
			if err == nil {
				err = e
			}
		case "multipart/form-data":
			// handled by ParseMultipartForm
		default:
			return &badStringError{"unknown Content-Type", ct}
		}
	}
	return err
}

// ParseMultipartForm parses a request body as multipart/form-data.
// The whole request body is parsed and up to a total of maxMemory bytes of
// its file parts are stored in memory, with the remainder stored on
// disk in temporary files.
// ParseMultipartForm calls ParseForm if necessary.
// After one call to ParseMultipartForm, subsequent calls have no effect.
func (r *Request) ParseMultipartForm(maxMemory int64) os.Error {
	if r.Form == nil {
		err := r.ParseForm()
		if err != nil {
			return err
		}
	}
	if r.MultipartForm != nil {
		return nil
	}
	if r.MultipartForm == multipartByReader {
		return os.NewError("http: multipart handled by MultipartReader")
	}

	mr, err := r.multipartReader()
	if err == ErrNotMultipart {
		return nil
	} else if err != nil {
		return err
	}

	f, err := mr.ReadForm(maxMemory)
	if err != nil {
		return err
	}
	for k, v := range f.Value {
		r.Form[k] = append(r.Form[k], v...)
	}
	r.MultipartForm = f

	return nil
}

// FormValue returns the first value for the named component of the query.
// FormValue calls ParseMultipartForm and ParseForm if necessary.
func (r *Request) FormValue(key string) string {
	if r.Form == nil {
		r.ParseMultipartForm(defaultMaxMemory)
	}
	if vs := r.Form[key]; len(vs) > 0 {
		return vs[0]
	}
	return ""
}

// FormFile returns the first file for the provided form key.
// FormFile calls ParseMultipartForm and ParseForm if necessary.
func (r *Request) FormFile(key string) (multipart.File, *multipart.FileHeader, os.Error) {
	if r.MultipartForm == multipartByReader {
		return nil, nil, os.NewError("http: multipart handled by MultipartReader")
	}
	if r.MultipartForm == nil {
		err := r.ParseMultipartForm(defaultMaxMemory)
		if err != nil {
			return nil, nil, err
		}
	}
	if r.MultipartForm != nil && r.MultipartForm.File != nil {
		if fhs := r.MultipartForm.File[key]; len(fhs) > 0 {
			f, err := fhs[0].Open()
			return f, fhs[0], err
		}
	}
	return nil, nil, ErrMissingFile
}

func (r *Request) expectsContinue() bool {
	return strings.ToLower(r.Header.Get("Expect")) == "100-continue"
}

func (r *Request) wantsHttp10KeepAlive() bool {
	if r.ProtoMajor != 1 || r.ProtoMinor != 0 {
		return false
	}
	return strings.Contains(strings.ToLower(r.Header.Get("Connection")), "keep-alive")
}
