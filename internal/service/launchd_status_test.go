package service

import (
	"context"
	"strings"
	"testing"
)

var launchdStatusBG = context.Background()

func TestLaunchdStatusShowsEnabled(t *testing.T) {
	te := newTestEnv(t, "darwin")
	m, _ := te.launchd(t)
	if err := m.Install(launchdStatusBG, Options{NoStart: true}); err != nil {
		t.Fatal(err)
	}
	te.resetOutput()
	if st, err := m.Status(launchdStatusBG, Options{}); err != nil || st != Stopped {
		t.Fatalf("status: %v %v", st, err)
	}
	if !strings.Contains(te.out.String(), "enabled: yes") {
		t.Errorf("status:\n%s", te.out.String())
	}
}

func TestLaunchdStatusShowsDisabled(t *testing.T) {
	te := newTestEnv(t, "darwin")
	m, fl := te.launchd(t)
	if err := m.Install(launchdStatusBG, Options{NoStart: true}); err != nil {
		t.Fatal(err)
	}
	fl.disabled = true
	before := fl.called("launchctl enable")
	te.resetOutput()
	st, err := m.Status(launchdStatusBG, Options{})
	if err != nil || st != Stopped {
		t.Fatalf("status: %v %v", st, err)
	}
	if !strings.Contains(te.out.String(), "enabled: NO") {
		t.Errorf("status:\n%s", te.out.String())
	}
	if got := fl.called("launchctl enable"); got != before {
		t.Errorf("status must not enable: %d calls (was %d)", got, before)
	}
}

func TestLaunchdStatusEnabledUnknownWhenPrintDisabledFails(t *testing.T) {
	te := newTestEnv(t, "darwin")
	m, fl := te.launchd(t)
	if err := m.Install(launchdStatusBG, Options{NoStart: true}); err != nil {
		t.Fatal(err)
	}
	fl.printDisabledFails = true
	te.resetOutput()
	if _, err := m.Status(launchdStatusBG, Options{}); err != nil {
		t.Fatalf("status: %v", err)
	}
	if !strings.Contains(te.out.String(), "enabled: unknown") {
		t.Errorf("status:\n%s", te.out.String())
	}
}

func TestParseLaunchdDisabled(t *testing.T) {
	cases := []struct {
		name string
		out  string
		want string
	}{
		{"newer enabled", "disabled services = {\n\t\"other.label\" => disabled\n\t\"" + LaunchdLabel + "\" => enabled\n}\n", "enabled"},
		{"newer disabled", "disabled services = {\n\t\"" + LaunchdLabel + "\" => disabled\n}\n", "disabled"},
		{"older false", "\t\"" + LaunchdLabel + "\" => false\n", "enabled"},
		{"older true", "\t\"" + LaunchdLabel + "\" => true\n", "disabled"},
		{"label absent", "disabled services = {\n\t\"other.label\" => disabled\n}\n", ""},
		{"empty", "", ""},
		{"garbage", "launchctl: could not print\n", ""},
	}
	for _, c := range cases {
		if got := parseLaunchdDisabled(c.out); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestLaunchdInstallClearsDisabled(t *testing.T) {
	te := newTestEnv(t, "darwin")
	m, fl := te.launchd(t)
	fl.disabled = true
	if err := m.Install(launchdStatusBG, Options{NoStart: true}); err != nil {
		t.Fatal(err)
	}
	if fl.disabled {
		t.Error("install must clear the disable override")
	}
	te.resetOutput()
	_, _ = m.Status(launchdStatusBG, Options{})
	if !strings.Contains(te.out.String(), "enabled: yes") {
		t.Errorf("status:\n%s", te.out.String())
	}
}
