package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

type processIdentity struct {
	group     int
	session   int
	state     byte
	startTime uint64
}

func main() {
	leader := flag.Int("leader", 0, "expected session leader PID")
	startTime := flag.Uint64("start-time", 0, "expected leader start time")
	signalName := flag.String("signal", "TERM", "signal to send (TERM or KILL)")
	excludeGroup := flag.Int("exclude-group", 0, "process group to leave untouched")
	targetOnly := flag.Bool("target-only", false, "signal only the identified PID")
	flag.Parse()

	if *leader <= 0 || *startTime == 0 {
		fatalf("leader and start-time are required")
	}
	signal, err := parseSignal(*signalName)
	if err != nil {
		fatalf("%v", err)
	}
	leaderIdentity, err := readIdentity(*leader)
	if err != nil {
		fatalf("read session leader: %v", err)
	}
	if leaderIdentity.startTime != *startTime {
		fatalf("session leader identity changed")
	}
	leaderPidfd, err := unix.PidfdOpen(*leader, 0)
	if err != nil {
		fatalf("open session leader pidfd: %v", err)
	}
	defer unix.Close(leaderPidfd)
	verifyTarget(*leader, *startTime)
	if *targetOnly {
		if err := unix.PidfdSendSignal(leaderPidfd, signal, nil, 0); err != nil && !errors.Is(err, unix.ESRCH) {
			fatalf("signal target: %v", err)
		}
		return
	}
	if leaderIdentity.group != *leader || leaderIdentity.session != *leader {
		fatalf("identified process is not a session leader")
	}

	entries, err := os.ReadDir("/proc")
	if err != nil {
		fatalf("read /proc: %v", err)
	}
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid == os.Getpid() || pid == *leader {
			continue
		}
		identity, err := readIdentity(pid)
		if err != nil || identity.session != *leader || identity.group == *excludeGroup || identity.state == 'Z' || identity.state == 'X' {
			continue
		}

		pidfd, err := unix.PidfdOpen(pid, 0)
		if errors.Is(err, unix.ESRCH) {
			continue
		}
		if err != nil {
			fatalf("open pidfd for %d: %v", pid, err)
		}

		verifySessionLeader(*leader, *startTime)
		current, currentErr := readIdentity(pid)
		if currentErr == nil && current.startTime == identity.startTime && current.session == *leader && current.group == identity.group {
			err = unix.PidfdSendSignal(pidfd, signal, nil, 0)
		}
		closeErr := unix.Close(pidfd)
		if err != nil && !errors.Is(err, unix.ESRCH) {
			fatalf("signal pid %d: %v", pid, err)
		}
		if closeErr != nil {
			fatalf("close pidfd for %d: %v", pid, closeErr)
		}
	}
	if leaderIdentity.group != *excludeGroup {
		if err := unix.PidfdSendSignal(leaderPidfd, signal, nil, 0); err != nil && !errors.Is(err, unix.ESRCH) {
			fatalf("signal session leader: %v", err)
		}
	}
}

func verifyTarget(pid int, startTime uint64) {
	identity, err := readIdentity(pid)
	if err != nil || identity.startTime != startTime {
		fatalf("process identity changed")
	}
}

func verifySessionLeader(pid int, startTime uint64) {
	identity, err := readIdentity(pid)
	if err != nil || identity.startTime != startTime || identity.group != pid || identity.session != pid {
		fatalf("session leader identity changed")
	}
}

func parseSignal(name string) (unix.Signal, error) {
	switch strings.ToUpper(name) {
	case "TERM":
		return unix.SIGTERM, nil
	case "KILL":
		return unix.SIGKILL, nil
	default:
		return 0, fmt.Errorf("unsupported signal %q", name)
	}
}

func readIdentity(pid int) (processIdentity, error) {
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return processIdentity{}, err
	}
	end := strings.LastIndex(string(data), ") ")
	if end < 0 {
		return processIdentity{}, errors.New("malformed process stat")
	}
	fields := strings.Fields(string(data[end+2:]))
	if len(fields) < 20 || len(fields[0]) != 1 {
		return processIdentity{}, errors.New("incomplete process stat")
	}
	group, err := strconv.Atoi(fields[2])
	if err != nil {
		return processIdentity{}, fmt.Errorf("parse process group: %w", err)
	}
	session, err := strconv.Atoi(fields[3])
	if err != nil {
		return processIdentity{}, fmt.Errorf("parse session: %w", err)
	}
	startTime, err := strconv.ParseUint(fields[19], 10, 64)
	if err != nil {
		return processIdentity{}, fmt.Errorf("parse start time: %w", err)
	}
	return processIdentity{group: group, session: session, state: fields[0][0], startTime: startTime}, nil
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "harness signal: "+format+"\n", args...)
	os.Exit(1)
}
