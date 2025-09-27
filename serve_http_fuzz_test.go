package traefik_warp

import "testing"

func FuzzExtractClientIP(f *testing.F) {
    seeds := []string{
        "1.2.3.4",
        "1.2.3.4:80",
        "[2001:db8::1]",
        "[2001:db8::1]:443",
        "2001:db8::1",            // bare IPv6
        "2001:db8::1:443",        // tricky IPv6-ish
        "garbage",
        "",
    }
    for _, s := range seeds {
        f.Add(s)
    }
    f.Fuzz(func(t *testing.T, s string) {
        _ = extractClientIP(s) // must not panic
    })
}

func FuzzParseSocketIP(f *testing.F) {
    seeds := []string{
        "203.0.113.10:443",
        "[2001:db8::1]:8443",
        "203.0.113.10",
        "[2001:db8::1]",
        ":443",
        "",
    }
    for _, s := range seeds {
        f.Add(s)
    }
    f.Fuzz(func(t *testing.T, s string) {
        _ = parseSocketIP(s) // must not panic
    })
}
