package egress

import (
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseBuildsReviewedProxyEnvironmentAndRedactionValues(t *testing.T) {
	config, err := Parse(
		"http://proxy-user:proxy-password@127.0.0.1:22334",
		"localhost,127.0.0.1,postgres,.service.internal",
	)
	if err != nil {
		t.Fatalf("parse proxy: %v", err)
	}
	values := config.GitEnvironment()
	if values["HTTP_PROXY"] != values["HTTPS_PROXY"] ||
		values["HTTP_PROXY"] != values["http_proxy"] ||
		values["HTTPS_PROXY"] != values["https_proxy"] ||
		values["NO_PROXY"] != "localhost,127.0.0.1,postgres,.service.internal" ||
		values["NO_PROXY"] != values["no_proxy"] {
		t.Fatalf("unexpected Git environment: %#v", values)
	}
	joined := strings.Join(config.SensitiveValues(), "\n")
	for _, secret := range []string{"proxy-user", "proxy-password"} {
		if !strings.Contains(joined, secret) {
			t.Fatalf("missing redaction value for %s", secret)
		}
	}
}

func TestParseRejectsUnsafeProxyAndNoProxyValues(t *testing.T) {
	for _, test := range []struct {
		name    string
		proxy   string
		noProxy string
	}{
		{name: "scheme", proxy: "socks5://127.0.0.1:1080"},
		{name: "host", proxy: "http:///missing-host"},
		{name: "port", proxy: "http://127.0.0.1:70000"},
		{name: "path", proxy: "http://127.0.0.1:8080/proxy"},
		{name: "query", proxy: "http://127.0.0.1:8080?secret=value"},
		{name: "public bypass", proxy: "http://127.0.0.1:8080", noProxy: "github.com"},
		{name: "wildcard bypass", proxy: "http://127.0.0.1:8080", noProxy: "*"},
		{name: "public CIDR", proxy: "http://127.0.0.1:8080", noProxy: "0.0.0.0/0"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := Parse(test.proxy, test.noProxy); err == nil {
				t.Fatal("expected invalid proxy configuration")
			}
		})
	}
}

func TestHTTPClientUsesOnlyTheExplicitRepoProxy(t *testing.T) {
	proxied := make(chan *http.Request, 1)
	proxy := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Proxy-Authorization") !=
			"Basic "+base64.StdEncoding.EncodeToString([]byte("proxy-user:proxy-password")) {
			t.Error("proxy credentials were not carried by the HTTP transport")
		}
		proxied <- request.Clone(request.Context())
		response.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(response, `{"ok":true}`)
	}))
	defer proxy.Close()

	proxyURL := strings.Replace(proxy.URL, "http://", "http://proxy-user:proxy-password@", 1)
	config, err := Parse(proxyURL, "localhost,127.0.0.1,::1")
	if err != nil {
		t.Fatalf("parse proxy: %v", err)
	}
	response, err := config.HTTPClient().Get("http://api.github.test/repos/acme/model")
	if err != nil {
		t.Fatalf("request through proxy: %v", err)
	}
	_ = response.Body.Close()
	request := <-proxied
	if request.URL.Host != "api.github.test" || request.URL.Path != "/repos/acme/model" {
		t.Fatalf("proxy saw unexpected target: %s", request.URL.String())
	}
}

func TestHTTPClientWithoutRepoProxyIgnoresProcessProxy(t *testing.T) {
	unexpectedProxy := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("process proxy should not receive Repo traffic")
	}))
	defer unexpectedProxy.Close()
	t.Setenv("HTTP_PROXY", unexpectedProxy.URL)
	t.Setenv("HTTPS_PROXY", unexpectedProxy.URL)

	target := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusNoContent)
	}))
	defer target.Close()
	config, err := Parse("", "localhost,127.0.0.1,::1")
	if err != nil {
		t.Fatalf("parse direct config: %v", err)
	}
	response, err := config.HTTPClient().Get(target.URL)
	if err != nil {
		t.Fatalf("direct request: %v", err)
	}
	_ = response.Body.Close()
}
