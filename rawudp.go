package main

// original from
// https://gist.githubusercontent.com/chrisnc/0ff3d1c20cb6687454b0/raw/1d9f5a1b6c9dd016e50eabe9e6863da4623614df/rawudp.go

import (
	"bytes"
	"encoding/binary"
	pb "github.com/jayalane/go-relay/udpProxy"
	"golang.org/x/sys/unix"
	"net"
	"runtime"
	"strconv"
)

type iphdr struct {
	vhl   uint8
	tos   uint8
	iplen uint16
	id    uint16
	off   uint16
	ttl   uint8
	proto uint8
	csum  uint16
	src   [4]byte
	dst   [4]byte
}

type udphdr struct {
	src  uint16
	dst  uint16
	ulen uint16
	csum uint16
}

// pseudo header used for checksum calculation
type pseudohdr struct {
	ipsrc   [4]byte
	ipdst   [4]byte
	zero    uint8
	ipproto uint8
	plen    uint16
}

func checksum(buf []byte) uint16 {
	sum := uint32(0)

	for ; len(buf) >= 2; buf = buf[2:] {
		sum += uint32(buf[0])<<8 | uint32(buf[1])
	}
	if len(buf) > 0 {
		sum += uint32(buf[0]) << 8
	}
	for sum > 0xffff {
		sum = (sum >> 16) + (sum & 0xffff)
	}
	csum := ^uint16(sum)
	/*
	 * From RFC 768:
	 * If the computed checksum is zero, it is transmitted as all ones (the
	 * equivalent in one's complement arithmetic). An all zero transmitted
	 * checksum value means that the transmitter generated no checksum (for
	 * debugging or for higher level protocols that don't care).
	 */
	if csum == 0 {
		csum = 0xffff
	}
	return csum
}

func (h *iphdr) checksum() {
	h.csum = 0
	var b bytes.Buffer
	binary.Write(&b, binary.BigEndian, h)
	h.csum = checksum(b.Bytes())
}

func (u *udphdr) checksum(ip *iphdr, payload []byte) {
	u.csum = 0
	phdr := pseudohdr{
		ipsrc:   ip.src,
		ipdst:   ip.dst,
		zero:    0,
		ipproto: ip.proto,
		plen:    u.ulen,
	}
	var b bytes.Buffer
	binary.Write(&b, binary.BigEndian, &phdr)
	binary.Write(&b, binary.BigEndian, u)
	binary.Write(&b, binary.BigEndian, &payload)
	u.csum = checksum(b.Bytes())
}

func sendRaw(m *pb.UdpMsg) {
	var err error
	ml.La("Sending ", m)

	ipSrcStr := m.SrcIp
	udpSrcStr := m.SrcPort
	srcPort, err := strconv.Atoi(udpSrcStr)
	if err != nil {
		ml.Ls("invalid source port:", m, udpSrcStr)
		return // oh well
	}

	ipDstStr := m.DstIp
	udpDstStr := m.DstPort
	dstPort, err := strconv.Atoi(udpDstStr)
	if err != nil {
		ml.Ls("invalid destination port:", m, udpDstStr)
		return // oh well
	}
	udpSrc := uint(srcPort)
	udpDst := uint(dstPort)

	ipSrc := net.ParseIP(ipSrcStr)
	if ipSrc == nil {
		ml.Ls("invalid source IP", m, ipSrcStr)
		return
	}
	ipDst := net.ParseIP(ipDstStr)
	if ipDst == nil {
		ml.Ls("invalid destination IP", m, ipDstStr)
		return
	}

	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_RAW, unix.IPPROTO_RAW)

	if err != nil || fd < 0 {
		ml.Ls("error creating a raw socket:", m, err)
		return
	}

	err = unix.SetsockoptInt(fd, unix.IPPROTO_IP, unix.IP_HDRINCL, 1)
	if err != nil {
		ml.Ls("error enabling IP_HDRINCL:", m, err)
		unix.Close(fd)
		return
	}

	ip := iphdr{
		vhl:   0x45,
		tos:   0,
		id:    0x1234, // the kernel overwrites id if it is zero
		off:   0,
		ttl:   64,
		proto: unix.IPPROTO_UDP,
	}
	copy(ip.src[:], ipSrc.To4())
	copy(ip.dst[:], ipDst.To4())
	// iplen and csum set later

	udp := udphdr{
		src: uint16(udpSrc),
		dst: uint16(udpDst),
	}
	// ulen and csum set later

	// just use an empty IPv4 sockaddr for Sendto
	// the kernel will route the packet based on the IP header
	addr := unix.SockaddrInet4{}

	payload := m.Msg
	udplen := 8 + len(payload)
	totalLen := 20 + udplen
	if totalLen > 0xffff {
		ml.Ls("message is too large to fit into a packet:", m, totalLen)
		return
	}
	// the kernel will overwrite the IP checksum, so this is included just for
	// completeness
	ip.iplen = uint16(totalLen)
	ip.checksum()

	// the kernel doesn't touch the UDP checksum, so we can either set it
	// correctly or leave it zero to indicate that we didn't use a checksum
	udp.ulen = uint16(udplen)
	udp.checksum(&ip, payload)

	var b bytes.Buffer
	err = binary.Write(&b, binary.BigEndian, &ip)
	if err != nil {
		ml.Ls("error encoding the IP header:", m, err)
		return
	}
	err = binary.Write(&b, binary.BigEndian, &udp)
	if err != nil {
		ml.Ls("error encoding the UDP header:", m, err)
		return
	}
	err = binary.Write(&b, binary.BigEndian, &payload)
	if err != nil {
		ml.Ls("error encoding the payload", m, err)
		return
	}
	bb := b.Bytes()

	/*
	 * For some reason, the IP header's length field needs to be in host byte order
	 * in OS X.
	 */
	if runtime.GOOS == "darwin" {
		bb[2], bb[3] = bb[3], bb[2]
	}

	err = unix.Sendto(fd, bb, 0, &addr)
	if err != nil {
		ml.Ls("error sending the packet:", m, err)
		return
	}
	ml.Ls("send bytes were sent", len(m.Msg))
	err = unix.Close(fd)
	if err != nil {
		ml.Ls("error closing the socket:", m, err)
		return
	}
}
