package main

import (
	"context"
	"crypto/rand"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

type ipv6SupportCache struct {
	lastChecked time.Time
	supported   bool
}

type HttpClient struct {
	*http.Client
	Ipv6Block string
	cache     map[string]ipv6SupportCache
	mu        sync.Mutex
}

func (client *HttpClient) OnRequest(req *http.Request) {
	if strings.Contains(req.URL.String(), "youtubei/v1/") {
		req.Header.Set("Content-Type", "application/json")
		ivs, ok := req.Context().Value(VisitorDataContextKey).(string)
		if ok && ivs != "" {
			req.Header.Set("x-goog-visitor-id", ivs)
			if len(ivs) > 50 {
				ivs = ivs[:50]
			}
			slog.Debug("Setting x-goog-visitor-id", "visitor_id", ivs)

		}
	}
	if strings.Contains(req.URL.String(), "music.youtube.com") {
		req.Header.Set("x-origin", "https://music.youtube.com")
	} else {
		req.Header.Set("origin", "https://www.youtube.com")
	}

	// close the tcp connection after request to rotate the ipv6 address
	req.Header.Set("Connection", "close")
	req.Header.Set("Cookie", "SOCS=CAI;")
	req.Header.Set(
		"User-Agent",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/142.0.0.0 Safari/537.36",
	)
}

func (client *HttpClient) Do(req *http.Request) (*http.Response, error) {
	if req != nil {
		client.OnRequest(req)
	}
	return client.Client.Do(req)
}

func (client *HttpClient) IsIpv6Supported(network, addr string) bool {
	ipv6Supported := false
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		slog.Error("Failed to split host & port", "error", err)
		return ipv6Supported
	} else {
		slog.Debug("splitted host and port", "host", host, "port", port)
	}

	ips, err := net.LookupIP(host)
	if err != nil {
		slog.Error("failed to lookup ip", "error", err)
		return ipv6Supported
	} else {
		slog.Debug("host ip lookup results", "ips", ips, "host", host)
		for _, ip := range ips {
			if ip.To4() == nil {
				ipv6Supported = true
				break
			}
		}
	}
	slog.Debug("ipv6 support check result", "addr", addr, "ipv6_supported", ipv6Supported)
	return ipv6Supported

}

func (client *HttpClient) GenerateRandomIpV6() string {
	_, ipNet, err := net.ParseCIDR(client.Ipv6Block)
	if err != nil {
		slog.Error("Failed to parse ipv6 subnet", "subnet", client.Ipv6Block, "error", err)
		return ""
	}

	base := ipNet.IP.To16() // each block in an ipv6 address is 16 bit (=2byte) (total 8 block)
	// [u16]:[u16]:[u16]:[u16]:[u16]:[u16]:[u16]:[u16]
	if base == nil {
		slog.Error("not an ipv6 network", "subnet", client.Ipv6Block)
		return ""
	}

	// returns the prefix size like /64, /48 etc
	prefixLen, _ := ipNet.Mask.Size()

	if prefixLen < 0 || prefixLen > 128 {
		slog.Error("invalid prefix length", "prefix_length", prefixLen)
		return ""
	}

	if prefixLen%16 != 0 {
		slog.Error("prefix length is not multiple of 16", "prefix_length", prefixLen)
		return ""
	}

	// ipv6 address => 128 bit => 16 bytes
	// each block is 16 bits => 2 bytes
	ip := make([]byte, 16)
	// copied base to the ip as some starting blocks are already fixed by the subnet mask
	// e.g., in 2001:0db8:85a3:abcd::/64, first 64 bits are fixed
	// eg., in 2001:0db8:85a3::/48, first 48 bits are fixed so we have to fill rest of the 5 blocks randomly
	copy(ip, base)

	// eg /48 => first 6 bytes are fixed, we need to randomize last 10 bytes
	// 48 / 8 = 6 so 16 - 6 = 10 random bytes needed
	randBytes := make([]byte, 16-prefixLen/8)
	if _, err := rand.Read(randBytes); err != nil {
		slog.Error("Random read failed", "error", err)
		return ""
	}

	for i := range randBytes {
		// foe eg [fixed]:[fixed]:[fixed]:rand:rand:rand:rand:rand:rand
		// prefixLen = 48 => prefixLen/8 = 6
		// so start filling from ip[6], ip[7], ip[8]...
		ip[prefixLen/8+i] = randBytes[i]
	}

	return net.IP(ip).String()
}

func (client *HttpClient) TransportDialContext(
	ctx context.Context,
	network string,
	addr string,
) (net.Conn, error) {
	slog.Debug("Connecting to Address", "addr", addr, "network", network)

	client.mu.Lock()
	defer client.mu.Unlock()

	ipv6Supported := false
	cached, ok := client.cache[addr]
	if !ok || time.Since(cached.lastChecked) > 30*time.Minute {
		fetched := client.IsIpv6Supported(network, addr)
		client.cache[addr] = ipv6SupportCache{
			lastChecked: time.Now(),
			supported:   fetched,
		}
		slog.Debug("ipv6 support cache updated", "addr", addr, "supported", cached)
		ipv6Supported = fetched
	} else {
		ipv6Supported = cached.supported
		slog.Debug("using cached ipv6 support value", "addr", addr, "supported", cached)
	}

	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	if ipv6Supported && client.Ipv6Block != "" {
		randomIpv6 := client.GenerateRandomIpV6()
		if randomIpv6 != "" {
			slog.Debug("selected outgoing ip address", slog.String("ipv6", randomIpv6))
			dialer.LocalAddr = &net.TCPAddr{
				IP:   net.ParseIP(randomIpv6),
				Port: 0,
			}
		} else {
			dialer.LocalAddr = nil
			slog.Debug("failed to generate random ipv6 address, using default local address")
		}

	} else {
		dialer.LocalAddr = nil
	}
	return dialer.DialContext(ctx, network, addr)
}

func NewHttpClient(timeoutSeconds int, ipv6Subnet string) *HttpClient {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	client := &HttpClient{Ipv6Block: ipv6Subnet, cache: make(map[string]ipv6SupportCache)}
	transport.DialContext = client.TransportDialContext
	client.Client = &http.Client{
		Timeout:   time.Duration(timeoutSeconds) * time.Second,
		Transport: transport,
	}
	return client
}
