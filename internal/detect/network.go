package detect

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"
)

// ProbeRemoteHost probes a saved remote host with its configured URLs.
func ProbeRemoteHost(ctx context.Context, host SavedHost) []ProbeResult {
	results := make([]ProbeResult, 0, len(host.ProbeURLs()))
	for _, u := range host.ProbeURLs() {
		r := Probe(ctx, u)
		results = append(results, r)
		if r.Backend != "" {
			break // stop at first successful identification
		}
	}
	return results
}

// NetworkProbeResult is the result of probing a discovered network machine.
type NetworkProbeResult struct {
	IP      string
	Backend string
	Name    string
}

// Netscan performs a TCP-connect sweep of private IPv4 subnets.
// Concurrency is limited to 64 at a time, with a 350ms timeout per port.
// Subnets larger than /22 are narrowed to the /24 around this machine.
type Netscan struct {
	Timeout     time.Duration
	Concurrency int
}

// NewNetscan creates a Netscan with default settings.
func NewNetscan() *Netscan {
	return &Netscan{
		Timeout:     350 * time.Millisecond,
		Concurrency: 64,
	}
}

// Run sweeps private subnets and returns discovered machines.
func (n *Netscan) Run(ctx context.Context, localIP string) ([]NetworkProbeResult, error) {
	subnets := privateSubnets(localIP)
	var results []NetworkProbeResult
	var mu sync.Mutex
	sem := make(chan struct{}, n.Concurrency)

	var wg sync.WaitGroup
	for _, subnet := range subnets {
		for i := 0; i < 256; i++ {
			ip := fmt.Sprintf("%s.%d", subnet, i)
			select {
			case <-ctx.Done():
				return results, ctx.Err()
			default:
			}
			wg.Add(1)
			sem <- struct{}{}
			go func(ip string) {
				defer wg.Done()
				defer func() { <-sem }()
				n.probeIP(ctx, ip, &results, &mu)
			}(ip)
		}
	}
	wg.Wait()
	return results, nil
}

func (n *Netscan) probeIP(ctx context.Context, ip string, results *[]NetworkProbeResult, mu *sync.Mutex) {
	for _, port := range []int{11434, 1234} {
		addr := net.JoinHostPort(ip, fmt.Sprintf("%d", port))
		conn, err := net.DialTimeout("tcp", addr, n.Timeout)
		if err != nil {
			continue
		}
		conn.Close()

		// Try to identify the backend.
		probeCtx, cancel := context.WithTimeout(ctx, n.Timeout)
		result := Probe(probeCtx, "http://"+addr)
		cancel()
		if result.Backend != "" {
			mu.Lock()
			*results = append(*results, NetworkProbeResult{
				IP:      ip,
				Backend: result.Backend,
				Name:    ip, // name resolved on round-trip
			})
			mu.Unlock()
		}
		return
	}
}

func privateSubnets(localIP string) []string {
	// Parse local IP to determine the /24.
	ip := net.ParseIP(localIP)
	if ip == nil {
		return []string{"192.168.1"}
	}
	ip4 := ip.To4()
	if ip4 == nil {
		return []string{"192.168.1"}
	}
	// Return just the /24 around this machine (narrowing from /22 per spec).
	return []string{fmt.Sprintf("%d.%d.%d", ip4[0], ip4[1], ip4[2])}
}
