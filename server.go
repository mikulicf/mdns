// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MIT

package mdns

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os/exec"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"github.com/miekg/dns"
)

const (
	ipv4mdns              = "224.0.0.251"
	ipv6mdns              = "ff02::fb"
	mdnsPort              = 5353
	forceUnicastResponses = false
)

var (
	ipv4Addr = &net.UDPAddr{
		IP:   net.ParseIP(ipv4mdns),
		Port: mdnsPort,
	}
	ipv6Addr = &net.UDPAddr{
		IP:   net.ParseIP(ipv6mdns),
		Port: mdnsPort,
	}
)

// Config is used to configure the mDNS server
type Config struct {
	// Zone must be provided to support responding to queries
	Zone Zone

	// Iface if provided binds the multicast listener to the given
	// interface. If not provided, the system default multicase interface
	// is used.
	Iface *net.Interface

	// LogEmptyResponses indicates the server should print an informative message
	// when there is an mDNS query for which the server has no response.
	LogEmptyResponses bool

	Ipv6 bool
}

// mDNS server is used to listen for mDNS queries and respond if we
// have a matching local record
type Server struct {
	config *Config

	ipv4List *net.UDPConn
	ipv6List *net.UDPConn

	shutdown   int32
	shutdownCh chan struct{}
}

type routeInfo struct {
	InterfaceIndex int
}

type interfaceInfo struct {
	NetConnectionID string
	MACAddress      string
}

// GetIPv4Addresses returns all IPv4 addresses of the system excluding loopback addresses.
func GetIPv4Addresses() ([]string, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}

	var ips []string
	for _, interf := range interfaces {
		addrs, err := interf.Addrs()
		if err != nil {
			return nil, err
		}

		for _, addr := range addrs {
			ip, _, err := net.ParseCIDR(addr.String())
			if err != nil {
				return nil, err
			}
			ipv4 := ip.To4()
			if ipv4 != nil && !toExclude(ipv4) {
				ips = append(ips, ipv4.String())
			}
		}
	}

	return ips, nil
}

func GetDefaultInterface() (*net.Interface, error) {
	var ifaceName string

	switch runtime.GOOS {
	case "windows":
		// Fetch the default route using WMI on Windows
		routeCmd := exec.Command("powershell", "-Command", "Get-WmiObject -Query 'SELECT * FROM Win32_IP4RouteTable WHERE Destination=\"0.0.0.0\"' | ConvertTo-Json")
		routeOutput, err := routeCmd.Output()
		if err != nil {
			return nil, err
		}
		var route routeInfo
		json.Unmarshal(routeOutput, &route)

		// Fetch the interface based on the InterfaceIndex from the default route
		ifaceCmd := exec.Command("powershell", "-Command", fmt.Sprintf("Get-WmiObject -Query 'SELECT * FROM Win32_NetworkAdapter WHERE InterfaceIndex=%d' | ConvertTo-Json", route.InterfaceIndex))
		ifaceOutput, err := ifaceCmd.Output()
		if err != nil {
			return nil, err
		}
		var winIfaceInfo interfaceInfo
		json.Unmarshal(ifaceOutput, &winIfaceInfo)

		ifaceName = winIfaceInfo.NetConnectionID

	case "linux":
		// Fetch the default route using `ip` command on Linux
		routeCmd := exec.Command("ip", "route", "list", "default")
		routeOutput, err := routeCmd.Output()
		if err != nil {
			return nil, err
		}
		fields := strings.Fields(string(routeOutput))
		for i, field := range fields {
			if field == "dev" && i+1 < len(fields) {
				ifaceName = fields[i+1]
				break
			}
		}
	}

	if ifaceName == "" {
		return nil, fmt.Errorf("default interface not found")
	}

	// Get the net.Interface based on the name
	iface, err := net.InterfaceByName(ifaceName)
	if err != nil {
		return nil, err
	}

	return iface, nil
}

// Checks if an IP is or loopback address.
func toExclude(ip net.IP) bool {
	privateIPBlocks := []*net.IPNet{
		{IP: net.IPv4(127, 0, 0, 0), Mask: net.CIDRMask(8, 32)},
	}
	for _, block := range privateIPBlocks {
		if block.Contains(ip) {
			return true
		}
	}
	return false
}
func NewServerWithTicker(config *Config, interval time.Duration) error {
	var server *Server
	registerServer := func() error {
		if server != nil {
			if err := server.Shutdown(); err != nil {
				return err
			}
		}
		var err error
		server, err = NewServer(config)
		if err != nil {
			return err
		}
		return nil
	}

	// Register server for the first time
	err := registerServer()
	if err != nil {
		return err
	}

	// Set up ticker for re-registering
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for range ticker.C {
		err := registerServer()
		if err != nil {
			return err
		}
	}
	return nil
}

// NewServer is used to create a new mDNS server from a config
func NewServer(config *Config) (*Server, error) {
	// Create the listeners
	ipv4List, err := net.ListenMulticastUDP("udp4", config.Iface, ipv4Addr)
	if err != nil {
		return nil, err
	}

	// Check if we have any listener
	if ipv4List == nil {
		return nil, fmt.Errorf("no multicast listeners could be started")
	}

	s := &Server{
		config:     config,
		ipv4List:   ipv4List,
		shutdownCh: make(chan struct{}),
	}

	if config.Ipv6 {
		ipv6List, err := net.ListenMulticastUDP("udp6", config.Iface, ipv6Addr)
		if err != nil {
			return nil, err
		}
		if ipv6List == nil {
			return nil, fmt.Errorf("no multicast listeners could be started")
		}

		s = &Server{
			ipv6List: ipv6List,
		}

		if ipv6List != nil {
			go s.recv(s.ipv6List)
		}

	}

	if ipv4List != nil {
		go s.recv(s.ipv4List)
	}

	return s, nil
}

// Shutdown is used to shutdown the listener
func (s *Server) Shutdown() error {
	if !atomic.CompareAndSwapInt32(&s.shutdown, 0, 1) {
		// something else already closed us
		return nil
	}

	close(s.shutdownCh)

	if s.ipv4List != nil {
		s.ipv4List.Close()
	}
	if s.config.Ipv6 {
		if s.ipv6List != nil {
			s.ipv6List.Close()
		}
	}
	return nil
}

// recv is a long running routine to receive packets from an interface
func (s *Server) recv(c *net.UDPConn) {
	if c == nil {
		return
	}
	buf := make([]byte, 65536)
	for atomic.LoadInt32(&s.shutdown) == 0 {
		n, from, err := c.ReadFrom(buf)

		if err != nil {
			continue
		}
		if err := s.parsePacket(buf[:n], from); err != nil {
			log.Printf("[ERR] mdns: Failed to handle query: %v", err)
		}
	}
}

// parsePacket is used to parse an incoming packet
func (s *Server) parsePacket(packet []byte, from net.Addr) error {
	var msg dns.Msg
	if err := msg.Unpack(packet); err != nil {
		log.Printf("[ERR] mdns: Failed to unpack packet: %v", err)
		return err
	}
	return s.handleQuery(&msg, from)
}

// handleQuery is used to handle an incoming query
func (s *Server) handleQuery(query *dns.Msg, from net.Addr) error {
	if query.Opcode != dns.OpcodeQuery {
		// "In both multicast query and multicast response messages, the OPCODE MUST
		// be zero on transmission (only standard queries are currently supported
		// over multicast).  Multicast DNS messages received with an OPCODE other
		// than zero MUST be silently ignored."  Note: OpcodeQuery == 0
		return fmt.Errorf("mdns: received query with non-zero Opcode %v: %v", query.Opcode, *query)
	}
	if query.Rcode != 0 {
		// "In both multicast query and multicast response messages, the Response
		// Code MUST be zero on transmission.  Multicast DNS messages received with
		// non-zero Response Codes MUST be silently ignored."
		return fmt.Errorf("mdns: received query with non-zero Rcode %v: %v", query.Rcode, *query)
	}

	// TODO(reddaly): Handle "TC (Truncated) Bit":
	//    In query messages, if the TC bit is set, it means that additional
	//    Known-Answer records may be following shortly.  A responder SHOULD
	//    record this fact, and wait for those additional Known-Answer records,
	//    before deciding whether to respond.  If the TC bit is clear, it means
	//    that the querying host has no additional Known Answers.
	if query.Truncated {
		return fmt.Errorf("[ERR] mdns: support for DNS requests with high truncated bit not implemented: %v", *query)
	}

	var unicastAnswer, multicastAnswer []dns.RR

	// Handle each question
	for _, q := range query.Question {
		mrecs, urecs := s.handleQuestion(q)
		multicastAnswer = append(multicastAnswer, mrecs...)
		unicastAnswer = append(unicastAnswer, urecs...)
	}

	// See section 18 of RFC 6762 for rules about DNS headers.
	resp := func(unicast bool) *dns.Msg {
		// 18.1: ID (Query Identifier)
		// 0 for multicast response, query.Id for unicast response
		id := uint16(0)
		if unicast {
			id = query.Id
		}

		var answer []dns.RR
		if unicast {
			answer = unicastAnswer
		} else {
			answer = multicastAnswer
		}
		if len(answer) == 0 {
			return nil
		}

		return &dns.Msg{
			MsgHdr: dns.MsgHdr{
				Id: id,

				// 18.2: QR (Query/Response) Bit - must be set to 1 in response.
				Response: true,

				// 18.3: OPCODE - must be zero in response (OpcodeQuery == 0)
				Opcode: dns.OpcodeQuery,

				// 18.4: AA (Authoritative Answer) Bit - must be set to 1
				Authoritative: true,

				// The following fields must all be set to 0:
				// 18.5: TC (TRUNCATED) Bit
				// 18.6: RD (Recursion Desired) Bit
				// 18.7: RA (Recursion Available) Bit
				// 18.8: Z (Zero) Bit
				// 18.9: AD (Authentic Data) Bit
				// 18.10: CD (Checking Disabled) Bit
				// 18.11: RCODE (Response Code)
			},
			// 18.12 pertains to questions (handled by handleQuestion)
			// 18.13 pertains to resource records (handled by handleQuestion)

			// 18.14: Name Compression - responses should be compressed (though see
			// caveats in the RFC), so set the Compress bit (part of the dns library
			// API, not part of the DNS packet) to true.
			Compress: true,

			Answer: answer,
		}
	}

	if s.config.LogEmptyResponses && len(multicastAnswer) == 0 && len(unicastAnswer) == 0 {
		questions := make([]string, len(query.Question))
		for i, q := range query.Question {
			questions[i] = q.Name
		}
		log.Printf("no responses for query with questions: %s", strings.Join(questions, ", "))
	}

	if mresp := resp(false); mresp != nil {
		if err := s.sendResponse(mresp, from, false); err != nil {
			return fmt.Errorf("mdns: error sending multicast response: %v", err)
		}
	}
	if uresp := resp(true); uresp != nil {
		if err := s.sendResponse(uresp, from, true); err != nil {
			return fmt.Errorf("mdns: error sending unicast response: %v", err)
		}
	}
	return nil
}

// handleQuestion is used to handle an incoming question
//
// The response to a question may be transmitted over multicast, unicast, or
// both.  The return values are DNS records for each transmission type.
func (s *Server) handleQuestion(q dns.Question) (multicastRecs, unicastRecs []dns.RR) {
	records := s.config.Zone.Records(q)

	if len(records) == 0 {
		return nil, nil
	}

	// Handle unicast and multicast responses.
	// TODO(reddaly): The decision about sending over unicast vs. multicast is not
	// yet fully compliant with RFC 6762.  For example, the unicast bit should be
	// ignored if the records in question are close to TTL expiration.  For now,
	// we just use the unicast bit to make the decision, as per the spec:
	//     RFC 6762, section 18.12.  Repurposing of Top Bit of qclass in Question
	//     Section
	//
	//     In the Question Section of a Multicast DNS query, the top bit of the
	//     qclass field is used to indicate that unicast responses are preferred
	//     for this particular question.  (See Section 5.4.)
	if q.Qclass&(1<<15) != 0 || forceUnicastResponses {
		return nil, records
	}
	return records, nil
}

// sendResponse is used to send a response packet
func (s *Server) sendResponse(resp *dns.Msg, from net.Addr, unicast bool) error {
	// TODO(reddaly): Respect the unicast argument, and allow sending responses
	// over multicast.
	buf, err := resp.Pack()
	if err != nil {
		return err
	}

	// Determine the socket to send from
	addr := from.(*net.UDPAddr)
	if addr.IP.To4() != nil {
		_, err = s.ipv4List.WriteToUDP(buf, addr)
		return err
	} else {
		if s.config.Ipv6 {
			_, err = s.ipv6List.WriteToUDP(buf, addr)
			return err
		}
	}
	return nil
}
