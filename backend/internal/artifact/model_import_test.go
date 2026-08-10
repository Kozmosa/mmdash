package artifact

import (
	"net"
	"net/url"
	"testing"
)

func TestValidatePublicHTTPSURLRejectsUnsafeTargets(t *testing.T) {
	for _, raw := range []string{
		"http://files.example/model.png",
		"https://user:password@files.example/model.png",
		"https://127.0.0.1/model.png",
		"https://10.0.0.1/model.png",
		"https://169.254.169.254/latest/meta-data",
		"https://[::1]/model.png",
		"https://files.example:8443/model.png",
	} {
		if _, err := validatePublicHTTPSURL(raw); err == nil {
			t.Errorf("unsafe URL accepted: %s", raw)
		}
	}
	if parsed, err := validatePublicHTTPSURL("https://files.example/model.png?expires=1"); err != nil || parsed.Hostname() != "files.example" {
		t.Fatalf("public HTTPS URL rejected: %v, %#v", err, parsed)
	}
}

func TestUnsafeModelIPIncludesPrivateAndSpecialRanges(t *testing.T) {
	for _, raw := range []string{"127.0.0.1", "10.1.2.3", "169.254.1.1", "224.0.0.1", "::", "::1", "fc00::1"} {
		if !unsafeModelIP(net.ParseIP(raw)) {
			t.Errorf("unsafe IP accepted: %s", raw)
		}
	}
	if unsafeModelIP(net.ParseIP("8.8.8.8")) {
		t.Fatal("public IP rejected")
	}
}

func TestModelImportIdentifiersAndFilenamesAreStableAndSafe(t *testing.T) {
	first := modelImportIdempotency("question", "block", "hash")
	second := modelImportIdempotency("question", "block", "hash")
	if first != second || first == modelImportIdempotency("question", "other", "hash") {
		t.Fatalf("unexpected Model import idempotency values: %q %q", first, second)
	}
	if got := safeModelFilename("../bad<name>.png"); got != "bad_name_.png" {
		t.Fatalf("safe filename = %q", got)
	}
}

func TestModelProxyAddressOnlyAllowsConfiguredProxyEndpoint(t *testing.T) {
	proxy, err := url.Parse("http://127.0.0.1:22334")
	if err != nil {
		t.Fatal(err)
	}
	if !modelProxyAddress("127.0.0.1:22334", proxy) {
		t.Fatal("configured local proxy endpoint was rejected")
	}
	for _, address := range []string{"127.0.0.1:8080", "10.0.0.1:22334", "files.example:443"} {
		if modelProxyAddress(address, proxy) {
			t.Fatalf("unconfigured proxy endpoint accepted: %s", address)
		}
	}
}
