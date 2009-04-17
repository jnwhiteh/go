// Copyright 2009 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package net

import (
	"net";
	"os";
	"syscall";
	"unsafe";
)

func v4ToSockaddr(p IP, port int) (sa1 *syscall.Sockaddr, err os.Error) {
	p = p.To4();
	if p == nil || port < 0 || port > 0xFFFF {
		return nil, os.EINVAL
	}
	sa := new(syscall.SockaddrInet4);
	sa.Len = syscall.SizeofSockaddrInet4;
	sa.Family = syscall.AF_INET;
	sa.Port[0] = byte(port>>8);
	sa.Port[1] = byte(port);
	for i := 0; i < IPv4len; i++ {
		sa.Addr[i] = p[i]
	}
	return (*syscall.Sockaddr)(unsafe.Pointer(sa)), nil
}

func v6ToSockaddr(p IP, port int) (sa1 *syscall.Sockaddr, err os.Error) {
	p = p.To16();
	if p == nil || port < 0 || port > 0xFFFF {
		return nil, os.EINVAL
	}
	sa := new(syscall.SockaddrInet6);
	sa.Len = syscall.SizeofSockaddrInet6;
	sa.Family = syscall.AF_INET6;
	sa.Port[0] = byte(port>>8);
	sa.Port[1] = byte(port);
	for i := 0; i < IPv6len; i++ {
		sa.Addr[i] = p[i]
	}
	return (*syscall.Sockaddr)(unsafe.Pointer(sa)), nil
}


func sockaddrToIP(sa1 *syscall.Sockaddr) (p IP, port int, err os.Error) {
	switch sa1.Family {
	case syscall.AF_INET:
		sa := (*syscall.SockaddrInet4)(unsafe.Pointer(sa1));
		a := IP(sa.Addr).To16();
		if a == nil {
			return nil, 0, os.EINVAL
		}
		return a, int(sa.Port[0])<<8 + int(sa.Port[1]), nil;
	case syscall.AF_INET6:
		sa := (*syscall.SockaddrInet6)(unsafe.Pointer(sa1));
		a := IP(sa.Addr).To16();
		if a == nil {
			return nil, 0, os.EINVAL
		}
		return nil, int(sa.Port[0])<<8 + int(sa.Port[1]), nil;
	default:
		return nil, 0, os.EINVAL
	}
	return nil, 0, nil	// not reached
}

func listenBacklog() int64 {
	return syscall.SOMAXCONN
}

