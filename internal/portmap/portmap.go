// Package portmap asks a home router to open a port for the server, or only
// whether it would.
package portmap

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// A home router opens a port for the server by itself when asked (decided
// 2026-10-17: reach the server directly before using a relay, with no router
// setting for the person). Two ways are asked, as most routers answer one:
// NAT-PMP (RFC 6886) at the gateway, and UPnP's Internet Gateway Device found
// by SSDP. Leafen asks for one TCP port, names it "Leafen", and asks again
// before the lease ends.

const (
	portMapLease        = time.Hour
	portMapDescription  = "Leafen"
	ssdpAddress         = "239.255.255.250:1900"
	natPMPPort          = 5351
	portMapReplyTimeout = 3 * time.Second
)

// PortMapper finds the gateway; the addresses are for development, where a
// stand-in answers instead of the network's own router.
type PortMapper struct {
	// SSDP is where the UPnP search goes: the multicast address when empty.
	SSDP string
	// Gateway is the NAT-PMP gateway: the first address of the server's
	// network when empty.
	Gateway string
}

// PortMapping is a port the router opened.
type PortMapping struct {
	Way          string
	ExternalIP   string
	ExternalPort int
	renew        func(context.Context) error
	remove       func(context.Context) error
}

// Renew asks the router to keep the port another lease.
func (mapping *PortMapping) Renew(ctx context.Context) error { return mapping.renew(ctx) }

// Remove asks the router to close the port.
func (mapping *PortMapping) Remove(ctx context.Context) error { return mapping.remove(ctx) }

// Probe says which ways the router answers — NAT-PMP's public address
// question and UPnP's gateway search — opening nothing (an agent setting up
// an address reads it; decided 2026-10-19).
func (mapper PortMapper) Probe(ctx context.Context) (natPMP bool, upnp bool) {
	if _, err := mapper.natPMP(ctx, []byte{0, 0}, 12); err == nil {
		natPMP = true
	}
	if _, _, err := mapper.discoverUPnP(ctx); err == nil {
		upnp = true
	}
	return natPMP, upnp
}

// Map asks the router to send an external TCP port to this computer's
// internal one, by NAT-PMP first and then UPnP.
func (mapper PortMapper) Map(ctx context.Context, internal, external int) (*PortMapping, error) {
	natPMP, natErr := mapper.mapNATPMP(ctx, internal, external)
	if natErr == nil {
		return natPMP, nil
	}
	upnp, upnpErr := mapper.mapUPnP(ctx, internal, external)
	if upnpErr == nil {
		return upnp, nil
	}
	return nil, fmt.Errorf("the router opened no port (NAT-PMP: %v; UPnP: %v)", natErr, upnpErr)
}

// localAddress is this computer's IPv4 address toward the network.
func localAddress() (net.IP, error) {
	conn, err := net.Dial("udp4", "192.0.2.1:9")
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	return conn.LocalAddr().(*net.UDPAddr).IP.To4(), nil
}

// gateway is the router NAT-PMP asks, guessed at .1 of the private (RFC 1918)
// network this computer's way out leaves by. It isn't the app's network reach
// table (internal/app/network_reach.go), which asks who opens an address: a
// tailnet or carrier-grade NAT address (100.64.0.0/10) or a link-local one,
// which that table counts as the network's, has no home router at .1 to ask.
func (mapper PortMapper) gateway() (string, error) {
	if mapper.Gateway != "" {
		return mapper.Gateway, nil
	}
	local, err := localAddress()
	if err != nil || local == nil || !local.IsPrivate() {
		return "", errors.New("no private network to find a gateway on")
	}
	gateway := net.IPv4(local[0], local[1], local[2], 1)
	return net.JoinHostPort(gateway.String(), strconv.Itoa(natPMPPort)), nil
}

func (mapper PortMapper) natPMP(ctx context.Context, request []byte, replyLength int) ([]byte, error) {
	gateway, err := mapper.gateway()
	if err != nil {
		return nil, err
	}
	conn, err := (&net.Dialer{}).DialContext(ctx, "udp4", gateway)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	reply := make([]byte, 16)
	// RFC 6886 asks again after 250 ms, doubling.
	for wait := 250 * time.Millisecond; wait <= time.Second; wait *= 2 {
		if _, err := conn.Write(request); err != nil {
			return nil, err
		}
		_ = conn.SetReadDeadline(time.Now().Add(wait))
		count, err := conn.Read(reply)
		if err == nil && count >= replyLength && reply[0] == 0 && reply[1] == request[1]+128 {
			if code := binary.BigEndian.Uint16(reply[2:4]); code != 0 {
				return nil, fmt.Errorf("the gateway refused (result %d)", code)
			}
			return reply[:count], nil
		}
	}
	return nil, errors.New("no NAT-PMP gateway answered")
}

func (mapper PortMapper) mapNATPMP(ctx context.Context, internal, external int) (*PortMapping, error) {
	address, err := mapper.natPMP(ctx, []byte{0, 0}, 12)
	if err != nil {
		return nil, err
	}
	externalIP := net.IP(address[8:12]).String()
	request := func(ctx context.Context, lease time.Duration) ([]byte, error) {
		message := make([]byte, 12)
		message[1] = 2 // TCP
		binary.BigEndian.PutUint16(message[4:6], uint16(internal))
		binary.BigEndian.PutUint16(message[6:8], uint16(external))
		binary.BigEndian.PutUint32(message[8:12], uint32(lease/time.Second))
		return mapper.natPMP(ctx, message, 16)
	}
	reply, err := request(ctx, portMapLease)
	if err != nil {
		return nil, err
	}
	if mapped := int(binary.BigEndian.Uint16(reply[10:12])); mapped != external {
		_, _ = request(ctx, 0)
		return nil, fmt.Errorf("the gateway gave port %d, not %d", mapped, external)
	}
	return &PortMapping{Way: "nat-pmp", ExternalIP: externalIP, ExternalPort: external,
		renew:  func(ctx context.Context) error { _, err := request(ctx, portMapLease); return err },
		remove: func(ctx context.Context) error { _, err := request(ctx, 0); return err }}, nil
}

// upnpDevice is the part of a device description that leads to the WAN
// connection service.
type upnpDevice struct {
	Services []struct {
		Type    string `xml:"serviceType"`
		Control string `xml:"controlURL"`
	} `xml:"serviceList>service"`
	Devices []upnpDevice `xml:"deviceList>device"`
}

func (device upnpDevice) wanConnection() (string, string) {
	for _, service := range device.Services {
		for _, kind := range []string{"WANIPConnection:2", "WANIPConnection:1", "WANPPPConnection:1"} {
			if strings.HasSuffix(service.Type, kind) {
				return service.Type, service.Control
			}
		}
	}
	for _, child := range device.Devices {
		if kind, control := child.wanConnection(); control != "" {
			return kind, control
		}
	}
	return "", ""
}

// discoverUPnP finds a gateway's WAN connection service: its type and
// control address.
func (mapper PortMapper) discoverUPnP(ctx context.Context) (string, string, error) {
	target := mapper.SSDP
	if target == "" {
		target = ssdpAddress
	}
	destination, err := net.ResolveUDPAddr("udp4", target)
	if err != nil {
		return "", "", err
	}
	conn, err := net.ListenUDP("udp4", nil)
	if err != nil {
		return "", "", err
	}
	defer conn.Close()
	search := "M-SEARCH * HTTP/1.1\r\nHOST: 239.255.255.250:1900\r\nMAN: \"ssdp:discover\"\r\nMX: 2\r\n" +
		"ST: urn:schemas-upnp-org:device:InternetGatewayDevice:1\r\n\r\n"
	if _, err := conn.WriteToUDP([]byte(search), destination); err != nil {
		return "", "", err
	}
	_ = conn.SetReadDeadline(time.Now().Add(portMapReplyTimeout))
	buffer := make([]byte, 2048)
	for {
		count, _, err := conn.ReadFromUDP(buffer)
		if err != nil {
			return "", "", errors.New("no UPnP gateway answered")
		}
		response, err := http.ReadResponse(bufio.NewReader(bytes.NewReader(buffer[:count])), nil)
		if err != nil {
			continue
		}
		location := response.Header.Get("Location")
		parsed, err := url.Parse(location)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			continue
		}
		description, err := fetchUPnPDescription(ctx, location)
		if err != nil {
			continue
		}
		kind, control := description.wanConnection()
		if control == "" {
			continue
		}
		controlURL, err := parsed.Parse(control)
		if err != nil {
			continue
		}
		return kind, controlURL.String(), nil
	}
}

func fetchUPnPDescription(ctx context.Context, location string) (upnpDevice, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, location, nil)
	if err != nil {
		return upnpDevice{}, err
	}
	response, err := (&http.Client{Timeout: portMapReplyTimeout}).Do(request)
	if err != nil {
		return upnpDevice{}, err
	}
	defer response.Body.Close()
	root := struct {
		Device upnpDevice `xml:"device"`
	}{}
	err = xml.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&root)
	return root.Device, err
}

// upnpCall calls one action of the WAN connection service.
func upnpCall(ctx context.Context, kind, control, action, arguments string) ([]byte, error) {
	body := `<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" ` +
		`s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/"><s:Body><u:` + action + ` xmlns:u="` + kind + `">` +
		arguments + `</u:` + action + `></s:Body></s:Envelope>`
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, control, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", `text/xml; charset="utf-8"`)
	request.Header.Set("SOAPAction", `"`+kind+`#`+action+`"`)
	response, err := (&http.Client{Timeout: portMapReplyTimeout}).Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	reply, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("the gateway refused %s (%d)", action, response.StatusCode)
	}
	return reply, nil
}

func xmlEscape(value string) string {
	var buffer bytes.Buffer
	_ = xml.EscapeText(&buffer, []byte(value))
	return buffer.String()
}

func (mapper PortMapper) mapUPnP(ctx context.Context, internal, external int) (*PortMapping, error) {
	kind, control, err := mapper.discoverUPnP(ctx)
	if err != nil {
		return nil, err
	}
	local, err := localAddress()
	if err != nil {
		return nil, err
	}
	add := func(ctx context.Context) error {
		_, err := upnpCall(ctx, kind, control, "AddPortMapping", fmt.Sprintf(
			"<NewRemoteHost></NewRemoteHost><NewExternalPort>%d</NewExternalPort><NewProtocol>TCP</NewProtocol>"+
				"<NewInternalPort>%d</NewInternalPort><NewInternalClient>%s</NewInternalClient><NewEnabled>1</NewEnabled>"+
				"<NewPortMappingDescription>%s</NewPortMappingDescription><NewLeaseDuration>%d</NewLeaseDuration>",
			external, internal, local, xmlEscape(portMapDescription), int(portMapLease/time.Second)))
		return err
	}
	if err := add(ctx); err != nil {
		return nil, err
	}
	externalIP := ""
	if reply, err := upnpCall(ctx, kind, control, "GetExternalIPAddress", ""); err == nil {
		found := struct {
			Address string `xml:"Body>GetExternalIPAddressResponse>NewExternalIPAddress"`
		}{}
		if xml.Unmarshal(reply, &found) == nil {
			externalIP = found.Address
		}
	}
	return &PortMapping{Way: "upnp", ExternalIP: externalIP, ExternalPort: external, renew: add,
		remove: func(ctx context.Context) error {
			_, err := upnpCall(ctx, kind, control, "DeletePortMapping", fmt.Sprintf(
				"<NewRemoteHost></NewRemoteHost><NewExternalPort>%d</NewExternalPort><NewProtocol>TCP</NewProtocol>", external))
			return err
		}}, nil
}

// GlobalIPv6 is this computer's global IPv6 address, or "" with none: one
// the internet routes to, which a unique local address (fc00::/7) isn't. It
// reads the address by itself rather than the app's network reach table,
// which this package, below the app, doesn't import.
func GlobalIPv6() string {
	conn, err := net.Dial("udp6", "[2001:db8::1]:9")
	if err != nil {
		return ""
	}
	defer conn.Close()
	ip := conn.LocalAddr().(*net.UDPAddr).IP
	if ip.To4() != nil || !ip.IsGlobalUnicast() || ip.IsPrivate() {
		return ""
	}
	return ip.String()
}
