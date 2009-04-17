// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package os

import syscall "syscall"

// An Error can represent any printable error condition.
type Error interface {
	String() string
}

// A helper type that can be embedded or wrapped to simplify satisfying
// Error.
type ErrorString string
func (e *ErrorString) String() string {
	return *e
}

// _Error is a structure wrapping a string describing an error.
// Errors are singleton structures, created by NewError, so their addresses can
// be compared to test for equality. A nil Error pointer means ``no error''.
// Use the String() method to get the contents; it handles the nil case.
// The Error type is intended for use by any package that wishes to define
// error strings.
type _Error struct {
	s string
}

// Indexed by errno.
// If we worry about syscall speed (only relevant on failure), we could
// make it an array, but it's probably not important.
var errorTab = make(map[int64] Error);

// Table of all known errors in system.  Use the same error string twice,
// get the same *os._Error.
var errorStringTab = make(map[string] Error);

// These functions contain a race if two goroutines add identical
// errors simultaneously but the consequences are unimportant.

// NewError allocates an Error object, but if s has been seen before,
// shares the _Error associated with that message.
func NewError(s string) Error {
	if s == "" {
		return nil
	}
	err, ok := errorStringTab[s];
	if ok {
		return err
	}
	err = &_Error{s};
	errorStringTab[s] = err;
	return err;
}

// ErrnoToError calls NewError to create an _Error object for the string
// associated with Unix error code errno.
func ErrnoToError(errno int64) Error {
	if errno == 0 {
		return nil
	}
	// Quick lookup by errno.
	err, ok := errorTab[errno];
	if ok {
		return err
	}
	err = NewError(syscall.Errstr(errno));
	errorTab[errno] = err;
	return err;
}

// Commonly known Unix errors.
var (
	// TODO(r):
	// 1. these become type ENONE struct { ErrorString }
	// 2. create private instances of each type: var eNONE ENONE(ErrnoToString(syscall.ENONE));
	// 3. put them in a table
	// 4. ErrnoToError uses the table. its error case ECATCHALL("%d")
	ENONE = ErrnoToError(syscall.ENONE);
	EPERM = ErrnoToError(syscall.EPERM);
	ENOENT = ErrnoToError(syscall.ENOENT);
	ESRCH = ErrnoToError(syscall.ESRCH);
	EINTR = ErrnoToError(syscall.EINTR);
	EIO = ErrnoToError(syscall.EIO);
	ENXIO = ErrnoToError(syscall.ENXIO);
	E2BIG = ErrnoToError(syscall.E2BIG);
	ENOEXEC = ErrnoToError(syscall.ENOEXEC);
	EBADF = ErrnoToError(syscall.EBADF);
	ECHILD = ErrnoToError(syscall.ECHILD);
	EDEADLK = ErrnoToError(syscall.EDEADLK);
	ENOMEM = ErrnoToError(syscall.ENOMEM);
	EACCES = ErrnoToError(syscall.EACCES);
	EFAULT = ErrnoToError(syscall.EFAULT);
	ENOTBLK = ErrnoToError(syscall.ENOTBLK);
	EBUSY = ErrnoToError(syscall.EBUSY);
	EEXIST = ErrnoToError(syscall.EEXIST);
	EXDEV = ErrnoToError(syscall.EXDEV);
	ENODEV = ErrnoToError(syscall.ENODEV);
	ENOTDIR = ErrnoToError(syscall.ENOTDIR);
	EISDIR = ErrnoToError(syscall.EISDIR);
	EINVAL = ErrnoToError(syscall.EINVAL);
	ENFILE = ErrnoToError(syscall.ENFILE);
	EMFILE = ErrnoToError(syscall.EMFILE);
	ENOTTY = ErrnoToError(syscall.ENOTTY);
	ETXTBSY = ErrnoToError(syscall.ETXTBSY);
	EFBIG = ErrnoToError(syscall.EFBIG);
	ENOSPC = ErrnoToError(syscall.ENOSPC);
	ESPIPE = ErrnoToError(syscall.ESPIPE);
	EROFS = ErrnoToError(syscall.EROFS);
	EMLINK = ErrnoToError(syscall.EMLINK);
	EPIPE = ErrnoToError(syscall.EPIPE);
	EDOM = ErrnoToError(syscall.EDOM);
	ERANGE = ErrnoToError(syscall.ERANGE);
	EAGAIN = ErrnoToError(syscall.EAGAIN);
)

// String returns the string associated with the _Error.
func (e *_Error) String() string {
	if e == nil {
		return "No _Error"
	}
	return e.s
}
