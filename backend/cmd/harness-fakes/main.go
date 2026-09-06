package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/sumia01/media-gate/internal/devharness"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:9091", "HTTP listen address")
	downloadRoot := flag.String("download-root", "", "allowed fake download root (required)")
	flag.Parse()
	if !isLoopbackAddress(*addr) {
		log.Fatalf("harness fake server must listen on a loopback address: %s", *addr)
	}

	handler, err := devharness.NewServer(*downloadRoot)
	if err != nil {
		log.Fatalf("create harness fake server: %v", err)
	}
	defer handler.Close()
	listener, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatalf("listen on %s: %v", *addr, err)
	}

	server := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("harness fake server shutdown: %v", err)
		}
	}()

	log.Printf("harness fakes listening on http://%s (download root %s)", listener.Addr(), *downloadRoot)
	if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("serve harness fakes: %v", err)
	}
}

func isLoopbackAddress(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
