// 通过内核 proc connector 在进程 exec 时立刻收到通知。
// 不在初始 pid 命名空间时内核会丢弃订阅，调用方应改用扫描。
package procproxy

import (
	"encoding/binary"
	"fmt"
	"os"
	"syscall"
	"time"
)

const (
	netlinkConnector = 11
	procEventExec    = 0x2
	procEventFork    = 0x1
)

// ProcSource 是新进程 pid 的来源。
type ProcSource struct {
	Mode string
	pids chan int
	stop func()
}

// Pids 返回 exec 的线程组 id。通道关闭表示订阅结束。
func (s *ProcSource) Pids() <-chan int {
	if s == nil || s.pids == nil {
		ch := make(chan int)
		close(ch)
		return ch
	}
	return s.pids
}

// Close 停止订阅。
func (s *ProcSource) Close() {
	if s != nil && s.stop != nil {
		s.stop()
	}
}

// ListenProcs 订阅进程 exec。内核不投递事件时返回错误，由监视循环改用扫描。
func ListenProcs() (*ProcSource, error) {
	fd, err := syscall.Socket(syscall.AF_NETLINK, syscall.SOCK_DGRAM|syscall.SOCK_CLOEXEC, netlinkConnector)
	if err != nil {
		return nil, fmt.Errorf("打开 netlink connector 失败: %w", err)
	}
	if err := syscall.Bind(fd, &syscall.SockaddrNetlink{Family: syscall.AF_NETLINK}); err != nil {
		syscall.Close(fd)
		return nil, fmt.Errorf("绑定 netlink connector 失败: %w", err)
	}
	var sa syscall.Sockaddr
	sa, err = syscall.Getsockname(fd)
	if err != nil {
		syscall.Close(fd)
		return nil, err
	}
	nl, ok := sa.(*syscall.SockaddrNetlink)
	if !ok {
		syscall.Close(fd)
		return nil, fmt.Errorf("netlink 地址类型异常")
	}
	if err := sendProcListen(fd, nl.Pid); err != nil {
		syscall.Close(fd)
		return nil, err
	}
	ch := make(chan int, 256)
	done := make(chan struct{})
	go func() {
		defer close(ch)
		buf := make([]byte, 8192)
		for {
			select {
			case <-done:
				return
			default:
			}
			n, _, err := syscall.Recvfrom(fd, buf, 0)
			if err != nil {
				select {
				case <-done:
					return
				default:
				}
				if err == syscall.EINTR {
					continue
				}
				return
			}
			for _, pid := range parseProcEvents(buf[:n]) {
				select {
				case ch <- pid:
				default:
				}
			}
		}
	}()
	return &ProcSource{
		Mode: "connector",
		pids: ch,
		stop: func() {
			close(done)
			syscall.Close(fd)
		},
	}, nil
}

// ConnectorWorks 在短时间内确认内核是否真的把 exec 事件送过来。
func ConnectorWorks(src *ProcSource) bool {
	if src == nil {
		return false
	}
	proc, err := os.StartProcess("/bin/true", []string{"true"}, &os.ProcAttr{})
	if err == nil {
		_, _ = proc.Wait()
	}
	timer := time.NewTimer(400 * time.Millisecond)
	defer timer.Stop()
	for {
		select {
		case _, ok := <-src.Pids():
			if !ok {
				return false
			}
			return true
		case <-timer.C:
			return false
		}
	}
}

func sendProcListen(fd int, port uint32) error {
	// proc_input: mcast_op=LISTEN, event_type=FORK|EXEC
	const dataLen = 8
	cn := 20 + dataLen
	total := syscall.NLMSG_HDRLEN + cn
	buf := make([]byte, total)
	binary.LittleEndian.PutUint32(buf[0:4], uint32(total))
	binary.LittleEndian.PutUint16(buf[4:6], uint16(syscall.NLMSG_DONE))
	binary.LittleEndian.PutUint32(buf[8:12], 1)
	binary.LittleEndian.PutUint32(buf[12:16], port)
	binary.LittleEndian.PutUint32(buf[16:20], 1) // CN_IDX_PROC
	binary.LittleEndian.PutUint32(buf[20:24], 1) // CN_VAL_PROC
	binary.LittleEndian.PutUint16(buf[32:34], dataLen)
	binary.LittleEndian.PutUint32(buf[36:40], 1) // PROC_CN_MCAST_LISTEN
	binary.LittleEndian.PutUint32(buf[40:44], procEventFork|procEventExec)
	return syscall.Sendto(fd, buf, 0, &syscall.SockaddrNetlink{Family: syscall.AF_NETLINK})
}

func parseProcEvents(buf []byte) []int {
	var pids []int
	for len(buf) >= syscall.NLMSG_HDRLEN {
		nlen := int(binary.LittleEndian.Uint32(buf[0:4]))
		if nlen < syscall.NLMSG_HDRLEN || nlen > len(buf) {
			break
		}
		msg := buf[syscall.NLMSG_HDRLEN:nlen]
		pids = append(pids, pidsFromCN(msg)...)
		nlen = (nlen + 3) &^ 3
		if nlen > len(buf) {
			break
		}
		buf = buf[nlen:]
	}
	return pids
}

func pidsFromCN(msg []byte) []int {
	if len(msg) < 20+16 {
		return nil
	}
	ev := msg[20:]
	what := binary.LittleEndian.Uint32(ev[0:4])
	if what != procEventExec || len(ev) < 24 {
		return nil
	}
	tgid := int(binary.LittleEndian.Uint32(ev[20:24]))
	if tgid <= 1 {
		return nil
	}
	return []int{tgid}
}
