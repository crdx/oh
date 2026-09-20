package fetch

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
)

func TestOnlyPublicAddressesAreReachable(t *testing.T) {
	for address, want := range map[string]bool{
		"93.184.216.34":     true,
		"2606:2800:220:1::": true,
		"2001:1::1":         true,
		"2001:3::1":         true,
		"2001:4:112::1":     true,
		"2001:20::1":        true,
		"2001:30::1":        true,
		"127.0.0.1":         false,
		"::1":               false,
		"::ffff:127.0.0.1":  false,
		"0.0.0.0":           false,
		"10.0.0.1":          false,
		"172.16.0.1":        false,
		"192.168.1.1":       false,
		"169.254.169.254":   false,
		"100.64.0.1":        false,
		"192.88.99.2":       false,
		"198.18.0.1":        false,
		"240.0.0.1":         false,
		"255.255.255.255":   false,
		"224.0.0.1":         false,
		"fd00::1":           false,
		"fe80::1":           false,
		"ff02::1":           false,
		"64:ff9b::7f00:1":   false,
		"64:ff9b:1::7f00:1": false,
		"100::1":            false,
		"100:0:0:1::1":      false,
		"2001:2::1":         false,
		"2001:5::1":         false,
		"2001:db8::1":       false,
		"2002::1":           false,
		"3fff::1":           false,
		"5f00::1":           false,
	} {
		isReachable := networkNameFor(netip.MustParseAddr(address)) == ""
		if isReachable != want {
			t.Errorf("%s reachable = %t, want %t", address, isReachable, want)
		}
	}
}

func TestARefusalNamesTheAddressAndWhatItIs(t *testing.T) {
	err := refuseReservedAddress("tcp", "169.254.169.254:80", nil)
	if err == nil {
		t.Fatal("expected the metadata address to be refused")
	}
	for _, want := range []string{"169.254.169.254", "link-local"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("expected the refusal to mention %q, got %v", want, err)
		}
	}

	if err := refuseReservedAddress("tcp", "93.184.216.34:443", nil); err != nil {
		t.Errorf("expected a public address to be allowed, got %v", err)
	}
}

func TestFetchWillNotReachALoopbackService(t *testing.T) {
	service := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte("<html><body>an internal secret</body></html>"))
	}))
	defer service.Close()

	output, err := fetchPage(t.Context(), defaultClient(), service.URL)
	if err == nil {
		t.Fatalf("expected the fetch to be refused, got %q", output.rawHTML)
	}
	if !strings.Contains(err.Error(), "reaches only the public internet") {
		t.Errorf("expected the refusal to say why, got %v", err)
	}
	if strings.Contains(string(output.rawHTML), "an internal secret") {
		t.Errorf("the loopback service was read: %q", output.rawHTML)
	}
}

func TestEveryConnectionIsJudgedRatherThanTheFirstRequest(t *testing.T) {
	transport := publicTransport()
	if transport.DialContext == nil {
		t.Fatal("the transport dials without the guard")
	}
	if transport.Proxy != nil {
		t.Fatal("the transport can bypass the guard through a proxy")
	}
}
