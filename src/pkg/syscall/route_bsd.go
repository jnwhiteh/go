// Copyright 2011 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Routing sockets and messages

package syscall

import (
	"unsafe"
)

const darwinAMD64 = OS == "darwin" && ARCH == "amd64"

// Round the length of a raw sockaddr up to align it properly.
func rsaAlignOf(salen int) int {
	salign := sizeofPtr
	// NOTE: It seems like 64-bit Darwin kernel still requires 32-bit
	// aligned access to BSD subsystem.
	if darwinAMD64 {
		salign = 4
	}
	if salen == 0 {
		return salign
	}
	return (salen + salign - 1) & ^(salign - 1)
}

// RouteRIB returns routing information base, as known as RIB,
// which consists of network facility information, states and
// parameters.
func RouteRIB(facility, param int) ([]byte, int) {
	var (
		tab []byte
		e   int
	)

	mib := []_C_int{CTL_NET, AF_ROUTE, 0, 0, _C_int(facility), _C_int(param)}

	// Find size.
	n := uintptr(0)
	if e = sysctl(mib, nil, &n, nil, 0); e != 0 {
		return nil, e
	}
	if n == 0 {
		return nil, 0
	}

	tab = make([]byte, n)
	if e = sysctl(mib, &tab[0], &n, nil, 0); e != 0 {
		return nil, e
	}

	return tab[:n], 0
}

// RoutingMessage represents a routing message.
type RoutingMessage interface {
	sockaddr() []Sockaddr
}

const anyMessageLen = unsafe.Sizeof(anyMessage{})

type anyMessage struct {
	Msglen  uint16
	Version uint8
	Type    uint8
}

func (any *anyMessage) toRoutingMessage(buf []byte) RoutingMessage {
	switch any.Type {
	case RTM_ADD, RTM_DELETE, RTM_CHANGE, RTM_GET, RTM_LOSING, RTM_REDIRECT, RTM_MISS, RTM_LOCK, RTM_RESOLVE:
		p := (*RouteMessage)(unsafe.Pointer(any))
		rtm := &RouteMessage{}
		rtm.Header = p.Header
		rtm.Data = buf[SizeofRtMsghdr:any.Msglen]
		return rtm
	case RTM_IFINFO:
		p := (*InterfaceMessage)(unsafe.Pointer(any))
		ifm := &InterfaceMessage{}
		ifm.Header = p.Header
		ifm.Data = buf[SizeofIfMsghdr:any.Msglen]
		return ifm
	case RTM_NEWADDR, RTM_DELADDR:
		p := (*InterfaceAddrMessage)(unsafe.Pointer(any))
		ifam := &InterfaceAddrMessage{}
		ifam.Header = p.Header
		ifam.Data = buf[SizeofIfaMsghdr:any.Msglen]
		return ifam
	case RTM_NEWMADDR, RTM_DELMADDR:
		// TODO: implement this in the near future
	}
	return nil
}

// RouteMessage represents a routing message containing routing
// entries.
type RouteMessage struct {
	Header RtMsghdr
	Data   []byte
}

func (m *RouteMessage) sockaddr() (sas []Sockaddr) {
	// TODO: implement this in the near future
	return nil
}

// InterfaceMessage represents a routing message containing
// network interface entries.
type InterfaceMessage struct {
	Header IfMsghdr
	Data   []byte
}

func (m *InterfaceMessage) sockaddr() (sas []Sockaddr) {
	if m.Header.Addrs&RTA_IFP == 0 {
		return nil
	}
	sa, e := anyToSockaddr((*RawSockaddrAny)(unsafe.Pointer(&m.Data[0])))
	if e != 0 {
		return nil
	}
	return append(sas, sa)
}

// InterfaceAddrMessage represents a routing message containing
// network interface address entries.
type InterfaceAddrMessage struct {
	Header IfaMsghdr
	Data   []byte
}

const rtaMask = RTA_IFA | RTA_NETMASK | RTA_BRD

func (m *InterfaceAddrMessage) sockaddr() (sas []Sockaddr) {
	if m.Header.Addrs&rtaMask == 0 {
		return nil
	}

	buf := m.Data[:]
	for i := uint(0); i < RTAX_MAX; i++ {
		if m.Header.Addrs&rtaMask&(1<<i) == 0 {
			continue
		}
		rsa := (*RawSockaddr)(unsafe.Pointer(&buf[0]))
		switch i {
		case RTAX_IFA:
			sa, e := anyToSockaddr((*RawSockaddrAny)(unsafe.Pointer(rsa)))
			if e != 0 {
				return nil
			}
			sas = append(sas, sa)
		case RTAX_NETMASK, RTAX_BRD:
			// nothing to do
		}
		buf = buf[rsaAlignOf(int(rsa.Len)):]
	}

	return sas
}

// ParseRoutingMessage parses buf as routing messages and returns
// the slice containing the RoutingMessage interfaces.
func ParseRoutingMessage(buf []byte) (msgs []RoutingMessage, errno int) {
	for len(buf) >= anyMessageLen {
		any := (*anyMessage)(unsafe.Pointer(&buf[0]))
		if any.Version != RTM_VERSION {
			return nil, EINVAL
		}
		msgs = append(msgs, any.toRoutingMessage(buf))
		buf = buf[any.Msglen:]
	}
	return msgs, 0
}

// ParseRoutingMessage parses msg's payload as raw sockaddrs and
// returns the slice containing the Sockaddr interfaces.
func ParseRoutingSockaddr(msg RoutingMessage) (sas []Sockaddr, errno int) {
	return append(sas, msg.sockaddr()...), 0
}
