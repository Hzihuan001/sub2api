//go:build unit

package service

import "testing"

func TestMonitorEndpointForKeyPrefersInternalResellerURL(t *testing.T) {
	t.Setenv("MOSHU_RESELLER_URL", "http://main:8080/")
	if got := monitorEndpointForKey("https://public.example", "sk-rs_test"); got != "http://main:8080" {
		t.Fatalf("reseller monitor endpoint = %q, want internal URL", got)
	}
	if got := monitorEndpointForKey("https://public.example", "sk-provider"); got != "https://public.example" {
		t.Fatalf("ordinary monitor endpoint changed unexpectedly: %q", got)
	}
}
