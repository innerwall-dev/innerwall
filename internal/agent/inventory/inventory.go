// Package inventory collects the descriptive facts the agent reports: host
// facts (hostname, operating system, interfaces and their addresses) and a
// best-effort list of listening services. Facts never confer identity or
// authorization (ADR-0016); they feed the inventory view and, for
// addresses, the resolution of peers in other workloads' rendered policy
// (ADR-0018).
package inventory

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
)

// Facts collects host facts. Every field is best effort: what cannot be
// read is left empty rather than failing the report.
func Facts() *innerwallv1.HostFacts {
	hostname, _ := os.Hostname()
	facts := &innerwallv1.HostFacts{Hostname: hostname, Os: osInfo()}
	ifaces, err := net.Interfaces()
	if err != nil {
		return facts
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		ni := &innerwallv1.NetworkInterface{Name: iface.Name, MacAddress: iface.HardwareAddr.String()}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ni.Addresses = append(ni.Addresses, a.String())
		}
		facts.Interfaces = append(facts.Interfaces, ni)
	}
	return facts
}

// osInfo reads what the host offers about itself: the release file on
// Linux, the kernel version from procfs, and the build's own view of the
// family and architecture.
func osInfo() *innerwallv1.OsInfo {
	info := &innerwallv1.OsInfo{Family: runtime.GOOS, Architecture: runtime.GOARCH}
	if b, err := os.ReadFile("/proc/sys/kernel/osrelease"); err == nil {
		info.KernelVersion = strings.TrimSpace(string(b))
	}
	if f, err := os.Open("/etc/os-release"); err == nil {
		defer func() { _ = f.Close() }()
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			key, value, ok := strings.Cut(sc.Text(), "=")
			if !ok {
				continue
			}
			value = strings.Trim(value, `"`)
			switch key {
			case "NAME":
				info.Name = value
			case "VERSION_ID":
				info.Version = value
			}
		}
	}
	return info
}

// ListeningServices discovers TCP listeners and bound UDP sockets from the
// host's network tables and attributes each to a process when the process
// table is readable. Limitations, by design rather than oversight:
//
//   - Only Linux procfs is read (/proc/net/{tcp,tcp6,udp,udp6}). On any
//     other host the list is empty, never an error.
//   - A UDP socket has no listening state; every bound UDP socket is
//     reported, which includes client sockets bound to ephemeral ports.
//   - Process attribution scans /proc/<pid>/fd for the socket's inode.
//     Reading another user's file descriptors needs privilege; where that
//     is missing the service is reported without a process. Attribution
//     is a snapshot and may name a process that has since exited.
//   - Sockets bound inside other network namespaces are not visible.
func ListeningServices() []*innerwallv1.ListeningService {
	tables := []struct {
		path  string
		proto innerwallv1.Protocol
		state string
	}{
		{"/proc/net/tcp", innerwallv1.Protocol_PROTOCOL_TCP, tcpListen},
		{"/proc/net/tcp6", innerwallv1.Protocol_PROTOCOL_TCP, tcpListen},
		{"/proc/net/udp", innerwallv1.Protocol_PROTOCOL_UDP, ""},
		{"/proc/net/udp6", innerwallv1.Protocol_PROTOCOL_UDP, ""},
	}
	var sockets []socket
	for _, t := range tables {
		f, err := os.Open(t.path)
		if err != nil {
			continue
		}
		sockets = append(sockets, parseTable(f, t.proto, t.state)...)
		_ = f.Close()
	}
	if len(sockets) == 0 {
		return nil
	}
	owners := socketOwners()
	seen := map[string]struct{}{}
	out := make([]*innerwallv1.ListeningService, 0, len(sockets))
	for _, s := range sockets {
		key := fmt.Sprintf("%d/%d", s.proto, s.port)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		svc := &innerwallv1.ListeningService{Protocol: s.proto, Port: s.port}
		if p, ok := owners[s.inode]; ok {
			svc.ProcessName, svc.ProcessPath = p.name, p.path
		}
		out = append(out, svc)
	}
	return out
}

// tcpListen is the TCP_LISTEN state as procfs prints it.
const tcpListen = "0A"

type socket struct {
	proto innerwallv1.Protocol
	port  uint32
	inode uint64
}

// parseTable reads one procfs socket table. Lines look like
//
//	sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
//	 0: 0100007F:0CEA 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 12345 ...
//
// The local port is the hex after the colon in local_address; state is the
// hex st column; inode is the tenth column. An empty wantState accepts
// every row.
func parseTable(r io.Reader, proto innerwallv1.Protocol, wantState string) []socket {
	var out []socket
	sc := bufio.NewScanner(r)
	first := true
	for sc.Scan() {
		if first {
			first = false
			continue
		}
		fields := strings.Fields(sc.Text())
		if len(fields) < 10 {
			continue
		}
		if wantState != "" && fields[3] != wantState {
			continue
		}
		_, portHex, ok := strings.Cut(fields[1], ":")
		if !ok {
			continue
		}
		portBytes, err := hex.DecodeString(portHex)
		if err != nil || len(portBytes) != 2 {
			continue
		}
		inode, err := strconv.ParseUint(fields[9], 10, 64)
		if err != nil {
			continue
		}
		out = append(out, socket{proto: proto, port: uint32(portBytes[0])<<8 | uint32(portBytes[1]), inode: inode})
	}
	return out
}

type owner struct {
	name string
	path string
}

// socketOwners maps socket inodes to the processes holding them by reading
// every /proc/<pid>/fd it is permitted to read.
func socketOwners() map[uint64]owner {
	owners := map[uint64]owner{}
	pids, err := os.ReadDir("/proc")
	if err != nil {
		return owners
	}
	for _, entry := range pids {
		pid := entry.Name()
		if pid[0] < '0' || pid[0] > '9' {
			continue
		}
		fdDir := filepath.Join("/proc", pid, "fd")
		fds, err := os.ReadDir(fdDir)
		if err != nil {
			continue
		}
		var own *owner
		for _, fd := range fds {
			target, err := os.Readlink(filepath.Join(fdDir, fd.Name()))
			if err != nil {
				continue
			}
			inode, ok := socketInode(target)
			if !ok {
				continue
			}
			if own == nil {
				o := owner{}
				if b, err := os.ReadFile(filepath.Join("/proc", pid, "comm")); err == nil { //nolint:gosec // procfs
					o.name = strings.TrimSpace(string(b))
				}
				if p, err := os.Readlink(filepath.Join("/proc", pid, "exe")); err == nil {
					o.path = p
				}
				own = &o
			}
			owners[inode] = *own
		}
	}
	return owners
}

// socketInode parses the "socket:[12345]" form of a socket descriptor link.
func socketInode(link string) (uint64, bool) {
	rest, ok := strings.CutPrefix(link, "socket:[")
	if !ok {
		return 0, false
	}
	num, ok := strings.CutSuffix(rest, "]")
	if !ok {
		return 0, false
	}
	inode, err := strconv.ParseUint(num, 10, 64)
	return inode, err == nil
}
