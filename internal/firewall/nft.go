package firewall

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"
)

// Runner executes nftables commands (replaced in tests).
type Runner interface {
	// Apply loads an nftables script atomically.
	Apply(script string) error
	// List returns the VegaSyncor table in JSON ("" if it does not exist).
	List() ([]byte, error)
}

// NFT runs the nft command.
type NFT struct{}

// Available reports whether nft is installed.
func Available() bool {
	_, err := exec.LookPath("nft")
	return err == nil
}

func (NFT) Apply(script string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "nft", "-f", "-")
	cmd.Stdin = strings.NewReader(script)
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return errors.New("nft: " + firstLine(msg))
	}
	return nil
}

func (NFT) List() ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "nft", "-j", "list", "table", "inet", Table)
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if strings.Contains(stderr.String(), "No such file or directory") {
			return nil, nil // table not present
		}
		return nil, errors.New("nft: " + firstLine(strings.TrimSpace(stderr.String())))
	}
	return out, nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// Counters reads the packet/byte counters of the VegaSyncor table, keyed by rule ID.
// It also reports whether the table exists.
func Counters(r Runner) (map[string][2]uint64, bool, error) {
	raw, err := r.List()
	if err != nil || raw == nil {
		return nil, false, err
	}
	return parseCounters(raw)
}

func parseCounters(raw []byte) (map[string][2]uint64, bool, error) {
	var doc struct {
		Nftables []map[string]json.RawMessage `json:"nftables"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, false, err
	}
	out := map[string][2]uint64{}
	exists := false
	for _, item := range doc.Nftables {
		if _, ok := item["table"]; ok {
			exists = true
		}
		rr, ok := item["rule"]
		if !ok {
			continue
		}
		var rule struct {
			Comment string            `json:"comment"`
			Expr    []json.RawMessage `json:"expr"`
		}
		if json.Unmarshal(rr, &rule) != nil || !strings.HasPrefix(rule.Comment, "vs:") {
			continue
		}
		id := strings.TrimPrefix(rule.Comment, "vs:")
		for _, e := range rule.Expr {
			var c struct {
				Counter *struct {
					Packets uint64 `json:"packets"`
					Bytes   uint64 `json:"bytes"`
				} `json:"counter"`
			}
			if json.Unmarshal(e, &c) == nil && c.Counter != nil {
				v := out[id]
				out[id] = [2]uint64{v[0] + c.Counter.Packets, v[1] + c.Counter.Bytes}
			}
		}
	}
	return out, exists, nil
}

// compactHosts removes duplicates and addresses already contained in a network
// of the list (nftables rejects sets with overlapping intervals).
func compactHosts(hosts []string) []string {
	type entry struct {
		s string
		n *net.IPNet
	}
	var es []entry
	seen := map[string]bool{}
	for _, h := range hosts {
		if seen[h] {
			continue
		}
		seen[h] = true
		n := toNet(h)
		if n == nil {
			continue
		}
		es = append(es, entry{h, n})
	}
	var out []string
	for i, a := range es {
		covered := false
		for j, b := range es {
			if i == j {
				continue
			}
			ao, _ := a.n.Mask.Size()
			bo, _ := b.n.Mask.Size()
			// a is covered by b if b contains a's network and b is larger (or equal and earlier)
			if b.n.Contains(a.n.IP) && (bo < ao || (bo == ao && j < i)) && len(a.n.IP) == len(b.n.IP) {
				covered = true
				break
			}
		}
		if !covered {
			out = append(out, a.s)
		}
	}
	sort.Strings(out)
	return out
}

func toNet(h string) *net.IPNet {
	if _, n, err := net.ParseCIDR(h); err == nil {
		return n
	}
	ip := net.ParseIP(h)
	if ip == nil {
		return nil
	}
	if v4 := ip.To4(); v4 != nil {
		return &net.IPNet{IP: v4, Mask: net.CIDRMask(32, 32)}
	}
	return &net.IPNet{IP: ip, Mask: net.CIDRMask(128, 128)}
}
