package main

import (
	"testing"
)

func TestRunRejectsExternalConfigurationPathFlags(t *testing.T) {
	for _, arguments := range [][]string{
		{"-config", `D:\outside\.env`},
		{"-root", `D:\outside`},
	} {
		if code := run(arguments); code != 2 {
			t.Fatalf("run(%q) exit code=%d, want 2", arguments, code)
		}
	}
}

func TestFirstRunHTTPDefaultsAllowPublicAccess(t *testing.T) {
	httpConfig, cors, allowedHosts := firstRunHTTPDefaults()
	if httpConfig.IPv4Host != "0.0.0.0" || httpConfig.IPv4Port != 3000 {
		t.Fatalf("first-run IPv4 listener = %s:%d, want 0.0.0.0:3000", httpConfig.IPv4Host, httpConfig.IPv4Port)
	}
	if httpConfig.IPv6Host != "::" || httpConfig.IPv6Port != 3000 {
		t.Fatalf("first-run IPv6 listener = [%s]:%d, want [::]:3000", httpConfig.IPv6Host, httpConfig.IPv6Port)
	}
	if !cors.AllowAllOrigins || len(cors.AllowedOrigins) != 0 || !cors.AllowCredentials {
		t.Fatalf("first-run CORS = %+v, want unrestricted credentialed origins", cors)
	}
	if len(cors.AllowedHeaders) != 1 || cors.AllowedHeaders[0] != "*" {
		t.Fatalf("first-run allowed headers = %v, want wildcard", cors.AllowedHeaders)
	}
	if !contains(cors.ExposedHeaders, "X-CSRF-Token") {
		t.Fatalf("first-run exposed headers = %v, want X-CSRF-Token", cors.ExposedHeaders)
	}
	if len(allowedHosts) != 0 {
		t.Fatalf("first-run allowed hosts = %v, want no host restriction", allowedHosts)
	}
}

func contains(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
