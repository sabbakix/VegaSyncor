package firewall

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"vegasyncor/internal/config"
)

func sampleFirewall() config.Firewall {
	f := config.DefaultFirewall()
	f.Enabled = true
	f.AdminHosts = []string{"192.168.1.20", "192.168.1.0/24", "fd00::5"}
	f.Rules = []config.FwRule{
		{ID: "r1", Direction: config.FwIn, Action: config.FwAllow, Proto: config.FwTCP, Port: "9100", Host: "10.0.0.7", Comment: "monitoring"},
		{ID: "r2", Direction: config.FwIn, Action: config.FwBlock, Proto: config.FwAny, Host: "192.168.1.66"},
		{ID: "r3", Direction: config.FwOut, Action: config.FwAllow, Proto: config.FwAny, Port: "8000-8100"},
	}
	return f
}

func TestScript(t *testing.T) {
	s := Script(Plan(sampleFirewall(), []string{"192.168.1.30", "192.168.1.31"}))
	for _, want := range []string{
		"table inet vegasyncor\ndelete table inet vegasyncor\n",
		"type filter hook input priority filter; policy drop;",
		"type filter hook output priority filter; policy drop;",
		// SSH only from the admin hosts; 192.168.1.20 is inside 192.168.1.0/24
		`ip saddr 192.168.1.0/24 tcp dport 22 counter accept comment "vs:in-ssh"`,
		`ip6 saddr fd00::5 tcp dport 22 counter accept comment "vs:in-ssh"`,
		`ip daddr { 192.168.1.30, 192.168.1.31 } tcp dport { 139, 445 } counter accept comment "vs:out-smb"`,
		`ip saddr 10.0.0.7 tcp dport 9100 counter accept comment "vs:rule-r1"`,
		`ip saddr 192.168.1.66 counter drop comment "vs:rule-r2"`,
		`meta l4proto { tcp, udp } th dport 8000-8100 counter accept comment "vs:rule-r3"`,
		`tcp dport { 80, 443 } counter accept comment "vs:out-web"`,
		`counter drop comment "vs:in-drop"`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("script without %q:\n%s", want, s)
		}
	}
	// block rules come before the SSH allow rule, so a blocked host cannot use SSH
	if strings.Index(s, "vs:rule-r2") > strings.Index(s, "vs:in-ssh") {
		t.Error("user block rule after the SSH rule")
	}
	if strings.Contains(s, "192.168.1.20") {
		t.Error("address covered by a network not removed")
	}
}

func TestScriptOptions(t *testing.T) {
	f := config.DefaultFirewall()
	f.AllowWeb, f.AllowPing = false, false
	s := Script(Plan(f, nil))
	if strings.Contains(s, "vs:out-web") || strings.Contains(s, "vs:in-ping") || strings.Contains(s, "vs:out-smb") {
		t.Errorf("disabled options still present:\n%s", s)
	}
	// no admin hosts: SSH from any host
	if !strings.Contains(s, `tcp dport 22 counter accept comment "vs:in-ssh"`) {
		t.Errorf("SSH rule missing:\n%s", s)
	}
}

func TestParseCounters(t *testing.T) {
	raw := []byte(`{"nftables":[{"metainfo":{}},{"table":{"family":"inet","name":"vegasyncor"}},
	 {"rule":{"chain":"input","comment":"vs:in-ssh","expr":[{"match":{}},{"counter":{"packets":5,"bytes":300}},{"accept":null}]}},
	 {"rule":{"chain":"input","comment":"vs:in-ssh","expr":[{"counter":{"packets":2,"bytes":100}},{"accept":null}]}},
	 {"rule":{"chain":"input","comment":"other","expr":[{"counter":{"packets":9,"bytes":9}}]}}]}`)
	c, exists, err := parseCounters(raw)
	if err != nil || !exists {
		t.Fatal(err, exists)
	}
	if c["in-ssh"] != [2]uint64{7, 400} || len(c) != 1 {
		t.Errorf("counters = %v", c)
	}
}

func TestCompactHosts(t *testing.T) {
	got := compactHosts([]string{"10.0.0.5", "10.0.0.0/24", "10.0.0.5", "10.1.0.1", "fd00::/64", "fd00::1"})
	want := "10.0.0.0/24 10.1.0.1 fd00::/64"
	if strings.Join(got, " ") != want {
		t.Errorf("compactHosts = %v, want %s", got, want)
	}
}

// TestScriptLoads loads the generated rules into a real kernel when an nft command
// is available, inside a throw-away user+network namespace (no effect on the host):
//
//	VEGASYNCOR_TEST_NFT="/path/to/nft" go test ./internal/firewall
func TestScriptLoads(t *testing.T) {
	nft := os.Getenv("VEGASYNCOR_TEST_NFT")
	if nft == "" {
		t.Skip("set VEGASYNCOR_TEST_NFT to the nft command to run this test")
	}
	f := filepath.Join(t.TempDir(), "rules.nft")
	script := Script(Plan(sampleFirewall(), []string{"192.168.1.30", "fd00::30"}))
	if err := os.WriteFile(f, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	// load twice: the second load must replace the table, not fail
	out, err := exec.Command("unshare", "-Urn", "sh", "-c", nft+" -f "+f+" && "+nft+" -f "+f+" && "+nft+" list table inet vegasyncor && "+nft+" -f - <<EOF\n"+DisableScript()+"EOF\n").CombinedOutput()
	if err != nil {
		t.Fatalf("nft rejected the rules: %v\n%s\n%s", err, out, script)
	}
	if !strings.Contains(string(out), `comment "vs:in-ssh"`) {
		t.Errorf("table not listed:\n%s", out)
	}
}

func TestParseSS(t *testing.T) {
	out := `tcp   LISTEN 0      4096         0.0.0.0:22        0.0.0.0:*     users:(("sshd",pid=101,fd=3))
tcp   LISTEN 0      4096            [::]:22           [::]:*     users:(("sshd",pid=101,fd=4))
tcp   LISTEN 0      4096       127.0.0.1:631       0.0.0.0:*     users:(("cupsd",pid=55,fd=7))
udp   UNCONN 0      0      127.0.0.53%lo:53        0.0.0.0:*     users:(("systemd-resolve",pid=9,fd=12))
tcp   ESTAB  0      0      192.168.1.10:22  192.168.1.20:51234   users:(("sshd",pid=900,fd=4))
tcp   ESTAB  0      0      192.168.1.10:40122 192.168.1.30:445
tcp   ESTAB  0      0         127.0.0.1:5000     127.0.0.1:41000 users:(("x",pid=1,fd=1))
tcp   TIME-WAIT 0   0      192.168.1.10:40100 192.168.1.30:445`
	l, c := parseSS(out)
	if len(l) != 4 || l[0].LocalPort != 22 || l[0].Process != "sshd" || l[0].LocalOnly {
		t.Fatalf("listening = %+v", l)
	}
	if !l[len(l)-1].LocalOnly {
		t.Errorf("loopback-only ports must come last: %+v", l)
	}
	if len(c) != 2 {
		t.Fatalf("connections = %+v", c)
	}
	if c[0].Direction != "in" || c[0].RemoteAddr != "192.168.1.20" || c[0].Process != "sshd" {
		t.Errorf("incoming SSH = %+v", c[0])
	}
	if c[1].Direction != "out" || c[1].RemotePort != 445 || c[1].Process != "" {
		t.Errorf("outgoing SMB (kernel) = %+v", c[1])
	}
}
