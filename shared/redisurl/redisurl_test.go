package redisurl

import "testing"

func TestRejectRESTCredentials(t *testing.T) {
	_, err := ParseClientOptions("https://us1-example-12345.upstash.io")
	if err == nil {
		t.Fatal("expected error for REST URL")
	}
}

func TestParseRedissURL(t *testing.T) {
	opt, err := ParseClientOptions("rediss://default:secret@us1-example-12345.upstash.io:6379")
	if err != nil {
		t.Fatal(err)
	}
	if opt.Addr == "" {
		t.Fatal("expected addr")
	}
}
