package service

import (
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
)

func TestParseProcAddrAndConflicts(t *testing.T) {
	ip, port, ok := parseProcAddr("0100007F:2382")
	if !ok || port != 9090 || !ip.Equal(net.IPv4(127, 0, 0, 1).To4()) {
		t.Fatalf("addr %v %d %v", ip, port, ok)
	}
	if !conflicts("127.0.0.1", 9090, net.IPv4(0, 0, 0, 0), 9090) {
		t.Fatal("unspecified listener conflicts with loopback bind")
	}
	if conflicts("127.0.0.1", 9090, net.IPv4(192, 168, 1, 2), 9090) {
		t.Fatal("other interface should not conflict")
	}
	sample := "  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n" +
		"   0: 0100007F:2382 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 4242 1 0000000000000000 100 0 0 10 0\n"
	got := parseTCPListeners(sample)
	if len(got) != 1 || got[0].port != 9090 || got[0].inode != "4242" {
		t.Fatalf("%+v", got)
	}
}

func TestFindListenerSeesOwnSocket(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	got, ok := findListener("127.0.0.1", port)
	if !ok {
		t.Fatal("listener not found")
	}
	if got.PID != os.Getpid() {
		t.Fatalf("pid %d cmd %q", got.PID, got.Cmd)
	}
	msg := occupantMessage("外部控制器", net.JoinHostPort("127.0.0.1", port))
	if !strings.Contains(msg, "已被占用") || !strings.Contains(msg, strconv.Itoa(os.Getpid())) {
		t.Fatal(msg)
	}
}
