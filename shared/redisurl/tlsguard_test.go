package redisurl

import (
	"crypto/tls"
	"testing"
)

func TestParseClientOptionsHardensRediss(t *testing.T) {
	opt, err := ParseClientOptions("rediss://example.upstash.io:6379")
	if err != nil {
		t.Fatal(err)
	}
	if opt.TLSConfig == nil || opt.TLSConfig.VerifyConnection == nil {
		t.Fatal("rediss TLS config not hardened")
	}
	if opt.TLSConfig.ServerName != "example.upstash.io" || opt.TLSConfig.MinVersion != tls.VersionTLS12 ||
		opt.TLSConfig.RootCAs != nil || opt.TLSConfig.InsecureSkipVerify {
		t.Fatal("go-redis TLS defaults changed")
	}
	plain, err := ParseClientOptions("redis://localhost:6379")
	if err != nil {
		t.Fatal(err)
	}
	if plain.TLSConfig != nil {
		t.Fatal("redis:// must stay without TLS")
	}
}
