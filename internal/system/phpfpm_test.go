package system

import (
	"strings"
	"testing"

	"github.com/reinis1996/repanel/internal/models"
)

func TestPerUserFPMNaming(t *testing.T) {
	if got := perUserFPMUnit(5, "8.3"); got != "repanel-php-8.3-u5.service" {
		t.Errorf("unit name = %q", got)
	}
	d := models.Domain{Name: "example.com", PHPVersion: "8.3", UserID: 5}
	if got := perUserPoolPath(d); got != "/etc/repanel/php-fpm/5/8.3/pool.d/repanel-example_com.conf" {
		t.Errorf("per-user pool path = %q", got)
	}
	if got := sharedPoolPath(d); got != "/etc/php/8.3/fpm/pool.d/repanel-example_com.conf" {
		t.Errorf("shared pool path = %q", got)
	}
}

func TestPerUserUnitInSlice(t *testing.T) {
	unit := renderPerUserUnit(7, "8.2")
	for _, want := range []string{
		"Slice=repanel-acct-7.slice",                                // bounded by the account slice
		"ExecStart=/usr/sbin/php-fpm8.2 --nodaemonize --fpm-config", // per-account master binary
		"--fpm-config /etc/repanel/php-fpm/7/8.2/php-fpm.conf",      // its own config
		"ExecReload=/bin/kill -USR2",                                // graceful pool reload
		"Type=notify",
	} {
		if !strings.Contains(unit, want) {
			t.Errorf("unit missing %q:\n%s", want, unit)
		}
	}
}

func TestPerUserMasterConfIncludesPools(t *testing.T) {
	conf := renderPerUserMasterConf(7, "8.2")
	if !strings.Contains(conf, "include = /etc/repanel/php-fpm/7/8.2/pool.d/*.conf") {
		t.Errorf("master conf doesn't include the account's pools:\n%s", conf)
	}
	if !strings.Contains(conf, "daemonize = no") {
		t.Errorf("master must run in the foreground for systemd:\n%s", conf)
	}
}
