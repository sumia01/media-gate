package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

type processIdentity struct {
	session int
	state   byte
}

func main() {
	if len(os.Args) < 2 {
		fatalf("command is required")
	}
	if err := unix.Prctl(unix.PR_SET_CHILD_SUBREAPER, 1, 0, 0, 0); err != nil {
		fatalf("enable child subreaper: %v", err)
	}

	command := exec.Command(os.Args[1], os.Args[2:]...)
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Start(); err != nil {
		fatalf("start command: %v", err)
	}
	directPID := command.Process.Pid
	directDone := false
	exitCode := 1

	signals := make(chan os.Signal, 2)
	signal.Notify(signals, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(signals)
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		for {
			pid, status, err := reapChild()
			if err != nil {
				fatalf("reap child: %v", err)
			}
			if pid == 0 {
				break
			}
			if pid == directPID {
				directDone = true
				exitCode = waitStatusExitCode(status)
				_ = command.Process.Release()
			}
		}

		if directDone && !sessionHasLiveDescendants(os.Getpid()) {
			os.Exit(exitCode)
		}

		select {
		case received := <-signals:
			sysSignal, ok := received.(syscall.Signal)
			if ok {
				signalSessionDescendants(os.Getpid(), unix.Signal(sysSignal))
			}
		case <-ticker.C:
		}
	}
}

func reapChild() (int, unix.WaitStatus, error) {
	var status unix.WaitStatus
	pid, err := unix.Wait4(-1, &status, unix.WNOHANG, nil)
	if errors.Is(err, unix.ECHILD) || errors.Is(err, unix.EINTR) {
		return 0, status, nil
	}
	return pid, status, err
}

func waitStatusExitCode(status unix.WaitStatus) int {
	if status.Exited() {
		return status.ExitStatus()
	}
	if status.Signaled() {
		return 128 + int(status.Signal())
	}
	return 1
}

func sessionHasLiveDescendants(session int) bool {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return false
	}
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid == session {
			continue
		}
		identity, err := readIdentity(pid)
		if err == nil && identity.session == session {
			return true
		}
	}
	return false
}

func signalSessionDescendants(session int, signal unix.Signal) {
	for _, pid := range sessionPIDs(session) {
		if pid == session {
			continue
		}
		pidfd, err := unix.PidfdOpen(pid, 0)
		if errors.Is(err, unix.ESRCH) {
			continue
		}
		if err != nil {
			continue
		}
		identity, identityErr := readIdentity(pid)
		if identityErr == nil && identity.session == session {
			_ = unix.PidfdSendSignal(pidfd, signal, nil, 0)
		}
		_ = unix.Close(pidfd)
	}
}

func sessionPIDs(session int) []int {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	pids := make([]int, 0, len(entries))
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		identity, err := readIdentity(pid)
		if err == nil && identity.session == session && identity.state != 'Z' && identity.state != 'X' {
			pids = append(pids, pid)
		}
	}
	return pids
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
	if len(fields) < 4 || len(fields[0]) != 1 {
		return processIdentity{}, errors.New("incomplete process stat")
	}
	session, err := strconv.Atoi(fields[3])
	if err != nil {
		return processIdentity{}, fmt.Errorf("parse session: %w", err)
	}
	return processIdentity{session: session, state: fields[0][0]}, nil
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "harness supervisor: "+format+"\n", args...)
	os.Exit(1)
}
