package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestValidatePasswordRules(t *testing.T) {
	for pw, want := range map[string]error{
		"":                              ErrPasswordTooShort,
		"short":                         ErrPasswordTooShort,
		"elevenchars":                   ErrPasswordTooShort,
		"twelve chars":                  nil,
		strings.Repeat("é", 12):         nil, // 12 characters, 24 bytes
		strings.Repeat("a", 72):         nil,
		strings.Repeat("a", 73):         ErrPasswordTooLong,
		strings.Repeat("é", 40):         ErrPasswordTooLong, // 80 bytes
		"change-me-change-me-change-me": nil,
	} {
		if got := ValidatePassword(pw); got != want {
			t.Errorf("ValidatePassword(%.20q) = %v, want %v", pw, got, want)
		}
	}
}

func TestHashAndCheckPassword(t *testing.T) {
	h, err := HashPassword("a long enough password")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$2") || strings.Contains(h, "long enough") {
		t.Fatalf("hash = %q", h)
	}
	if !CheckPassword(h, "a long enough password") {
		t.Error("the right password was rejected")
	}
	for _, bad := range []string{"", "a long enough passwor", "A long enough password", strings.Repeat("x", 100)} {
		if CheckPassword(h, bad) {
			t.Errorf("%.20q accepted", bad)
		}
	}
	if CheckPassword("", "a long enough password") || CheckPassword("not a hash", "a long enough password") {
		t.Error("an empty or malformed hash must never match")
	}
	if _, err := HashPassword("short"); err == nil {
		t.Error("HashPassword accepted a short password")
	}
	if err := ValidateHash(h); err != nil {
		t.Errorf("ValidateHash(real hash) = %v", err)
	}
	if err := ValidateHash("hunter2hunter2"); err == nil {
		t.Error("ValidateHash accepted a clear password")
	}
}

func TestSessionStoreIssueValidateExpireRevoke(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	s := NewSessionStore(time.Hour, func() time.Time { return now })
	tok, exp := s.Issue()
	if len(tok) != 43 || !exp.Equal(now.Add(time.Hour)) {
		t.Fatalf("token %q expires %v", tok, exp)
	}
	if !s.Valid(tok) || s.Valid("") || s.Valid(tok+"x") || s.Valid(strings.Repeat("a", 500)) {
		t.Fatal("Valid is wrong for a live / empty / altered / oversize token")
	}
	other, _ := s.Issue()
	if other == tok {
		t.Fatal("tokens must be unique")
	}
	s.RevokeAll()
	if s.Valid(tok) || s.Valid(other) {
		t.Fatal("RevokeAll left sessions alive")
	}
	tok, _ = s.Issue()
	now = now.Add(time.Hour)
	if s.Valid(tok) {
		t.Fatal("valid at the exact expiry instant")
	}
}

func TestSessionStoreIsBounded(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	s := NewSessionStore(time.Hour, func() time.Time { return now })
	var first string
	for i := 0; i < maxSessions+50; i++ {
		tok, _ := s.Issue()
		if i == 0 {
			first = tok
		}
		now = now.Add(time.Second)
	}
	if s.Len() > maxSessions {
		t.Fatalf("%d sessions held, limit %d", s.Len(), maxSessions)
	}
	if s.Valid(first) {
		t.Error("the oldest session should have been evicted first")
	}
}

func TestLoginLimiterBackoffDoublesAndCaps(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	l := NewLoginLimiter(func() time.Time { return now })
	const ip = "203.0.113.5"
	for i := 0; i < freeAttempts; i++ {
		if ok, _ := l.Allow(ip); !ok {
			t.Fatalf("attempt %d blocked inside the free allowance", i+1)
		}
		l.Fail(ip)
	}
	if ok, _ := l.Allow(ip); !ok {
		t.Fatal("blocked right after the free attempts")
	}
	var delays []time.Duration
	for i := 0; i < 12; i++ {
		l.Fail(ip)
		ok, wait := l.Allow(ip)
		if ok {
			t.Fatalf("failure %d did not lock the address", freeAttempts+1+i)
		}
		delays = append(delays, wait)
		now = now.Add(wait) // wait it out, then fail again
	}
	if delays[0] != 2*time.Second || delays[1] != 4*time.Second || delays[2] != 8*time.Second {
		t.Fatalf("delays = %v, want 2s, 4s, 8s...", delays)
	}
	if last := delays[len(delays)-1]; last != maxDelay {
		t.Fatalf("delay never capped: %v", delays)
	}
	l.Success(ip)
	if ok, _ := l.Allow(ip); !ok {
		t.Fatal("a success must clear the lockout")
	}
}

func TestLoginLimiterIsPerAddressAndForgets(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	l := NewLoginLimiter(func() time.Time { return now })
	for i := 0; i < freeAttempts+2; i++ {
		l.Fail("198.51.100.1")
	}
	if ok, _ := l.Allow("198.51.100.1"); ok {
		t.Fatal("address one should be locked")
	}
	if ok, _ := l.Allow("198.51.100.2"); !ok {
		t.Fatal("address two must be unaffected")
	}
	now = now.Add(2 * time.Hour)
	l.Fail("198.51.100.1") // history older than forgetAfter restarts the count
	if ok, _ := l.Allow("198.51.100.1"); !ok {
		t.Fatal("old failures should have been forgotten")
	}
}

func TestLoginLimiterGlobalWindowStopsAddressRotation(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	l := NewLoginLimiter(func() time.Time { return now })
	for i := 0; i <= globalMax; i++ {
		l.Fail("10.0.0." + string(rune('0'+i%10)) + string(rune('a'+i/10)))
	}
	if ok, wait := l.Allow("192.0.2.200"); ok || wait <= 0 {
		t.Fatalf("a never-seen address was allowed during a global lockout (%v)", wait)
	}
	now = now.Add(globalLock + time.Second)
	if ok, _ := l.Allow("192.0.2.200"); !ok {
		t.Fatal("the global lockout should end")
	}
}

func TestLoginLimiterIsBounded(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	l := NewLoginLimiter(func() time.Time { return now })
	for i := 0; i < maxTracked+500; i++ {
		l.Fail(strings.Repeat("x", 1) + string(rune(0x100+i)))
		now = now.Add(time.Millisecond)
	}
	if len(l.ips) > maxTracked {
		t.Fatalf("tracking %d addresses, limit %d", len(l.ips), maxTracked)
	}
}

func TestHostPolicy(t *testing.T) {
	p := NewHostPolicy([]string{".Example.com", "nas.lan"})
	for host, want := range map[string]bool{
		"localhost": true, "LOCALHOST:4648": true, "a.localhost": true, "127.0.0.1": true, "[::1]:80": true,
		"192.168.1.5:4648": true, "[fe80::1]": true, "example.com": true, "x.y.example.com": true, "nas.lan": true, "nas.lan:99": true,
		"evil.com": false, "localhost.evil.com": false, "notexample.com": false, "x.nas.lan": false, "": false, " ": false,
		"localhost:": false, "localhost:abc": false, "localhost:0": false, "localhost:99999": false, "a@b": false, "a/b": false,
	} {
		if got := p.Allowed(host); got != want {
			t.Errorf("Allowed(%q) = %v, want %v", host, got, want)
		}
	}
}

func TestIsTrueLoopback(t *testing.T) {
	mk := func(remote, host string, hdr ...string) *http.Request {
		r := httptest.NewRequest("GET", "http://"+host+"/api/token", nil)
		r.RemoteAddr = remote
		for i := 0; i+1 < len(hdr); i += 2 {
			r.Header.Set(hdr[i], hdr[i+1])
		}
		return r
	}
	cases := map[string]struct {
		r    *http.Request
		want bool
	}{
		"loopback peer + loopback host": {mk("127.0.0.1:5", "127.0.0.1:4648"), true},
		"ipv6 loopback":                 {mk("[::1]:5", "[::1]:4648"), true},
		"localhost name":                {mk("127.0.0.1:5", "localhost:4648"), true},
		"LAN peer":                      {mk("192.168.1.2:5", "127.0.0.1:4648"), false},
		"loopback peer, LAN host":       {mk("127.0.0.1:5", "192.168.1.9:4648"), false},
		"loopback peer, external host":  {mk("127.0.0.1:5", "smind.example.com"), false},
		"x-forwarded-for":               {mk("127.0.0.1:5", "localhost", "X-Forwarded-For", "1.2.3.4"), false},
		"x-forwarded-proto":             {mk("127.0.0.1:5", "localhost", "X-Forwarded-Proto", "https"), false},
		"forwarded":                     {mk("127.0.0.1:5", "localhost", "Forwarded", "for=1.2.3.4"), false},
		"x-real-ip":                     {mk("127.0.0.1:5", "localhost", "X-Real-Ip", "1.2.3.4"), false},
		"unparsable peer":               {mk("garbage", "localhost"), false},
	}
	for name, c := range cases {
		if got := IsTrueLoopback(c.r); got != c.want {
			t.Errorf("%s: IsTrueLoopback = %v, want %v", name, got, c.want)
		}
	}
}
