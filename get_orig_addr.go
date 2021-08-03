// get the original destination for the socket when redirect by linux iptables
// refer to https://raw.githubusercontent.com/missdeer/avege/master/src/inbound/redir/redir_iptables.go
//

package main

import (
	"errors"
	"fmt"
	"net"
	"syscall"
)

const (
	soOriginalDst     = 80
	iP6tSoOriginalDst = 80
)

func isEINTR(err error) bool {

	if err == nil {
		return false
	}
	errno, ok := err.(syscall.Errno)
	if ok && errno == syscall.EINTR {
		return true
	}
	return false
}

func getOriginalDst(clientConn *net.TCPConn) (rawaddr []byte, host string, newTCPConn *net.TCPConn, err error) {
	if clientConn == nil {
		err = errors.New("ERR: clientConn is nil")
		return
	}

	// test if the underlying fd is nil
	remoteAddr := clientConn.RemoteAddr()
	if remoteAddr == nil {
		err = errors.New("ERR: clientConn.fd is nil")
		return
	}

	newTCPConn = nil
	// net.TCPConn.File() will cause the receiver's (clientConn) socket to be placed in blocking mode.
	// The workaround is to take the File returned by .File(), do getsockopt() to get the original
	// destination, then create a new *net.TCPConn by calling net.Conn.FileConn().  The new TCPConn
	// will be in non-blocking mode.  What a pain.
	clientConnFile, err := clientConn.File()
	if err != nil {
		return
	}
	clientConn.Close()

	// Get original destination
	// this is the only syscall in the Golang libs that I can find that returns 16 bytes
	// Example result: &{Multiaddr:[2 0 31 144 206 190 36 45 0 0 0 0 0 0 0 0] Interface:0}
	// port starts at the 3rd byte and is 2 bytes long (31 144 = port 8080)
	// IPv6 version, didn't find a way to detect network family
	//addr, err := syscall.GetsockoptIPv6Mreq(int(clientConnFile.Fd()), syscall.IPPROTO_IPV6, IP6T_SO_ORIGINAL_DST)
	// IPv4 address starts at the 5th byte, 4 bytes long (206 190 36 45)
	// Chris - this needs to handle EINTR
	done := false
	var addr *syscall.IPv6Mreq
	for done != true {
		addr, err = syscall.GetsockoptIPv6Mreq(int(clientConnFile.Fd()), syscall.IPPROTO_IP, soOriginalDst)
		if err != nil {
			if isEINTR(err) {
				continue
			}
			ml.Ls("Error from getsockopt", err)
			// still have to redup the FDs
			var newConn net.Conn
			newConn, err = net.FileConn(clientConnFile)
			if err != nil {
				return
			}
			if _, ok := newConn.(*net.TCPConn); ok {
				newTCPConn = newConn.(*net.TCPConn)
				clientConnFile.Close()
			} else {
				err = errors.New("Some error with FD cloning")
				return
			}
			return
		}
		done = true
	}
	newConn, err := net.FileConn(clientConnFile)
	if err != nil {
		return
	}
	if _, ok := newConn.(*net.TCPConn); ok {
		newTCPConn = newConn.(*net.TCPConn)
		clientConnFile.Close()
	} else {
		err = errors.New("Some error with FD cloning")
		return
	}

	// \attention: IPv4 only!!!
	// address type, 1 - IPv4, 4 - IPv6, 3 - hostname, only IPv4 is supported now
	rawaddr = append(rawaddr, byte(1))
	// raw IP address, 4 bytes for IPv4 or 16 bytes for IPv6, only IPv4 is supported now
	rawaddr = append(rawaddr, addr.Multiaddr[4])
	rawaddr = append(rawaddr, addr.Multiaddr[5])
	rawaddr = append(rawaddr, addr.Multiaddr[6])
	rawaddr = append(rawaddr, addr.Multiaddr[7])
	// port
	rawaddr = append(rawaddr, addr.Multiaddr[2])
	rawaddr = append(rawaddr, addr.Multiaddr[3])

	host = fmt.Sprintf(
		"%d.%d.%d.%d:%d",
		addr.Multiaddr[4],
		addr.Multiaddr[5],
		addr.Multiaddr[6],
		addr.Multiaddr[7],
		uint16(addr.Multiaddr[2])<<8+uint16(addr.Multiaddr[3]),
	)

	return
}

func getOriginalDstUDP(clientConn *net.UDPConn) (rawaddr []byte,
	host string,
	newUDPConn *net.UDPConn,
	err error) {

	if clientConn == nil {
		err = errors.New("ERR: clientConn is nil")
		return
	}

	// test if the underlying fd is nil
	remoteAddr := clientConn.RemoteAddr()
	if remoteAddr == nil {
		err = errors.New("ERR: clientConn.fd is nil")
		return
	}

	newUDPConn = nil
	// net.UDPConn.File() will cause the receiver's (clientConn) socket to be placed in blocking mode.
	// The workaround is to take the File returned by .File(), do getsockopt() to get the original
	// destination, then create a new *net.UDPConn by calling net.Conn.FileConn().  The new UDPConn
	// will be in non-blocking mode.  What a pain.
	clientConnFile, err := clientConn.File()
	if err != nil {
		return
	}
	clientConn.Close()

	// Get original destination
	// this is the only syscall in the Golang libs that I can find that returns 16 bytes
	// Example result: &{Multiaddr:[2 0 31 144 206 190 36 45 0 0 0 0 0 0 0 0] Interface:0}
	// port starts at the 3rd byte and is 2 bytes long (31 144 = port 8080)
	// IPv6 version, didn't find a way to detect network family
	//addr, err := syscall.GetsockoptIPv6Mreq(int(clientConnFile.Fd()), syscall.IPPROTO_IPV6, IP6T_SO_ORIGINAL_DST)
	// IPv4 address starts at the 5th byte, 4 bytes long (206 190 36 45)
	// Chris - this needs to handle EINTR
	done := false
	var addr *syscall.IPv6Mreq
	for done != true {
		addr, err = syscall.GetsockoptIPv6Mreq(int(clientConnFile.Fd()), syscall.IPPROTO_IP, soOriginalDst)
		if err != nil {
			if isEINTR(err) {
				continue
			}
			ml.Ls("Error from getsockopt", err)
			// still have to redup the FDs
			var newConn net.Conn
			newConn, err = net.FileConn(clientConnFile)
			if err != nil {
				return
			}
			if _, ok := newConn.(*net.UDPConn); ok {
				newUDPConn = newConn.(*net.UDPConn)
				clientConnFile.Close()
			} else {
				err = errors.New("Some error with FD cloning")
				return
			}
			return
		}
		done = true
	}
	newConn, err := net.FileConn(clientConnFile)
	if err != nil {
		return
	}
	if _, ok := newConn.(*net.UDPConn); ok {
		newUDPConn = newConn.(*net.UDPConn)
		clientConnFile.Close()
	} else {
		err = errors.New("Some error with FD cloning")
		return
	}

	// \attention: IPv4 only!!!
	// address type, 1 - IPv4, 4 - IPv6, 3 - hostname, only IPv4 is supported now
	rawaddr = append(rawaddr, byte(1))
	// raw IP address, 4 bytes for IPv4 or 16 bytes for IPv6, only IPv4 is supported now
	rawaddr = append(rawaddr, addr.Multiaddr[4])
	rawaddr = append(rawaddr, addr.Multiaddr[5])
	rawaddr = append(rawaddr, addr.Multiaddr[6])
	rawaddr = append(rawaddr, addr.Multiaddr[7])
	// port
	rawaddr = append(rawaddr, addr.Multiaddr[2])
	rawaddr = append(rawaddr, addr.Multiaddr[3])

	host = fmt.Sprintf(
		"%d.%d.%d.%d:%d",
		addr.Multiaddr[4],
		addr.Multiaddr[5],
		addr.Multiaddr[6],
		addr.Multiaddr[7],
		uint16(addr.Multiaddr[2])<<8+uint16(addr.Multiaddr[3]),
	)

	return
}
