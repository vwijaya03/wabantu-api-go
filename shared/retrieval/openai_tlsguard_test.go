package retrieval

import (
	"net/http"
	"testing"
)

func TestOpenAIHTTPClientHardened(t *testing.T) {
	c := newOpenAIHTTPClient()
	tr, ok := c.Transport.(*http.Transport)
	if !ok || tr.TLSClientConfig == nil || tr.TLSClientConfig.VerifyConnection == nil {
		t.Fatal("openai transport not hardened")
	}
	if !tr.ForceAttemptHTTP2 || tr.MaxIdleConnsPerHost != 10 || tr.TLSClientConfig.RootCAs != nil || tr.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("openai transport settings changed")
	}
}
