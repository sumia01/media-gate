package main

import "testing"

func TestIsLoopbackAddress(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:9091", "[::1]:9091", "localhost:9091"} {
		if !isLoopbackAddress(addr) {
			t.Errorf("isLoopbackAddress(%q) = false", addr)
		}
	}
	for _, addr := range []string{"0.0.0.0:9091", ":9091", "192.0.2.1:9091", "invalid"} {
		if isLoopbackAddress(addr) {
			t.Errorf("isLoopbackAddress(%q) = true", addr)
		}
	}
}
