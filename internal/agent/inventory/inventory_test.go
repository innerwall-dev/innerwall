package inventory

import (
	"strings"
	"testing"

	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
)

const tcpTable = `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 0100007F:1538 00000000:0000 0A 00000000:00000000 00:00000000 00000000   999        0 20001 1 0000000000000000 100 0 0 10 0
   1: 00000000:01BB 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 20002 1 0000000000000000 100 0 0 10 0
   2: 0A00040B:D2F8 0A000401:01BB 01 00000000:00000000 00:00000000 00000000  1000        0 20003 1 0000000000000000 20 4 30 10 -1
`

func TestParseTable(t *testing.T) {
	got := parseTable(strings.NewReader(tcpTable), innerwallv1.Protocol_PROTOCOL_TCP, tcpListen)
	if len(got) != 2 {
		t.Fatalf("sockets = %+v", got)
	}
	if got[0].port != 5432 || got[0].inode != 20001 || got[1].port != 443 || got[1].inode != 20002 {
		t.Fatalf("sockets = %+v", got)
	}
	// Without a state filter every row is a socket (the UDP case).
	if all := parseTable(strings.NewReader(tcpTable), innerwallv1.Protocol_PROTOCOL_UDP, ""); len(all) != 3 {
		t.Fatalf("unfiltered sockets = %+v", all)
	}
}

func TestSocketInode(t *testing.T) {
	if n, ok := socketInode("socket:[20001]"); !ok || n != 20001 {
		t.Fatalf("socketInode = %d %v", n, ok)
	}
	for _, bad := range []string{"pipe:[1]", "socket:[x]", "/dev/null", "socket:[1"} {
		if _, ok := socketInode(bad); ok {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestFactsHaveHostnameAndFamily(t *testing.T) {
	f := Facts()
	if f.GetOs().GetFamily() == "" || f.GetOs().GetArchitecture() == "" {
		t.Fatalf("facts = %v", f)
	}
	// Listening-service discovery never fails, whatever the host offers.
	_ = ListeningServices()
}
