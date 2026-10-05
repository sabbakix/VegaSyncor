package firewall

import (
	"context"
	"net"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Socket is a listening port or an active connection of the server.
type Socket struct {
	Proto      string `json:"proto"` // tcp | udp
	State      string `json:"state"`
	LocalAddr  string `json:"local_addr"`
	LocalPort  int    `json:"local_port"`
	RemoteAddr string `json:"remote_addr,omitempty"`
	RemotePort int    `json:"remote_port,omitempty"`
	Process    string `json:"process,omitempty"` // "" = kernel (e.g. SMB mounts) or not visible
	PID        int    `json:"pid,omitempty"`
	Direction  string `json:"direction,omitempty"`  // connections: "in" | "out"
	LocalOnly  bool   `json:"local_only,omitempty"` // listening only on loopback
}

// Sockets lists listening ports and active connections using ss (iproute2).
func Sockets() (listening, connections []Socket, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ss", "-H", "-tunap")
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	out, err := cmd.Output()
	if err != nil {
		return nil, nil, err
	}
	l, c := parseSS(string(out))
	return l, c, nil
}

var procRe = regexp.MustCompile(`\("([^"]+)",pid=(\d+)`)

// parseSS parses the output of "ss -H -tunap".
func parseSS(out string) (listening, connections []Socket) {
	var raw []Socket
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 6 {
			continue
		}
		s := Socket{Proto: f[0], State: f[1]}
		if s.Proto != "tcp" && s.Proto != "udp" {
			continue
		}
		s.LocalAddr, s.LocalPort = splitAddr(f[4])
		s.RemoteAddr, s.RemotePort = splitAddr(f[5])
		if m := procRe.FindStringSubmatch(line); m != nil {
			s.Process = m[1]
			s.PID, _ = strconv.Atoi(m[2])
		}
		raw = append(raw, s)
	}

	listen := map[string]bool{} // proto/port of the listening sockets
	seen := map[string]bool{}
	for _, s := range raw {
		isListen := s.State == "LISTEN" || (s.Proto == "udp" && s.State == "UNCONN" && (s.RemotePort == 0))
		if !isListen {
			continue
		}
		s.LocalOnly = isLoopback(s.LocalAddr)
		s.RemoteAddr, s.RemotePort, s.State = "", 0, ""
		key := s.Proto + "/" + strconv.Itoa(s.LocalPort) + "/" + s.LocalAddr
		if seen[key] {
			continue
		}
		seen[key] = true
		listen[s.Proto+"/"+strconv.Itoa(s.LocalPort)] = true
		listening = append(listening, s)
	}
	for _, s := range raw {
		if s.State != "ESTAB" || isLoopback(s.RemoteAddr) {
			continue
		}
		s.Direction = "out"
		if listen[s.Proto+"/"+strconv.Itoa(s.LocalPort)] {
			s.Direction = "in"
		}
		connections = append(connections, s)
	}
	sort.SliceStable(listening, func(a, b int) bool {
		if listening[a].LocalOnly != listening[b].LocalOnly {
			return !listening[a].LocalOnly // reachable from the network first
		}
		if listening[a].LocalPort != listening[b].LocalPort {
			return listening[a].LocalPort < listening[b].LocalPort
		}
		return listening[a].Proto < listening[b].Proto
	})
	sort.SliceStable(connections, func(a, b int) bool {
		if connections[a].Direction != connections[b].Direction {
			return connections[a].Direction == "in"
		}
		return connections[a].RemoteAddr < connections[b].RemoteAddr
	})
	return listening, connections
}

// splitAddr splits "addr:port", "[v6]:port", "*:port" and "addr%iface:port".
func splitAddr(s string) (string, int) {
	i := strings.LastIndex(s, ":")
	if i < 0 {
		return s, 0
	}
	host, port := s[:i], s[i+1:]
	host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	if j := strings.Index(host, "%"); j >= 0 {
		host = host[:j]
	}
	if host == "*" {
		host = "0.0.0.0"
	}
	p, _ := strconv.Atoi(port)
	return host, p
}

func isLoopback(addr string) bool {
	ip := net.ParseIP(addr)
	return ip != nil && ip.IsLoopback()
}
