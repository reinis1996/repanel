// Command repctl is the command-line client for a RePanel instance. It talks to
// the panel's REST API using a personal API token (see "API Tokens" in the UI).
package main

import (
	"fmt"
	"os"
	"strings"
)

var version = "0.1.0"

const usage = `repctl — command-line client for RePanel

Usage:
  repctl [global flags] <command> [args]

Commands:
  login                 Save panel URL and token to the config file
  whoami                Show the authenticated account
  domains list          List websites
  domains create NAME   Create a website (flags: --php VERSION, --dns)
  domains delete ID     Delete a website
  dns list              List DNS zones
  databases list        List databases
  backups list          List backups
  traffic list          Show bandwidth per account
  tokens list           List your API tokens
  api METHOD PATH [BODY] Raw request, e.g. api GET /api/dashboard

Global flags:
  --url URL             Panel base URL        (env REPANEL_URL)
  --token TOKEN         API token             (env REPANEL_TOKEN)
  --insecure            Skip TLS verification  (env REPANEL_INSECURE=1)
  --json                Print raw JSON instead of tables
  --version             Print version and exit
  -h, --help            Show this help

Configuration precedence: flags > environment > config file
  (` + "config file: see `repctl login`" + `)
`

// globalFlags are the options that may precede the command.
type globalFlags struct {
	url      string
	token    string
	insecure bool
	json     bool
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error: "+err.Error())
		os.Exit(1)
	}
}

func run(args []string) error {
	var gf globalFlags
	var cmd string
	var rest []string

	// Consume leading global flags up to the command token.
	i := 0
	for ; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") {
			cmd = a
			rest = args[i+1:]
			break
		}
		name, val, hasVal := splitFlag(a)
		switch name {
		case "-h", "--help":
			fmt.Print(usage)
			return nil
		case "--version":
			fmt.Println("repctl " + version)
			return nil
		case "--insecure":
			gf.insecure = true
		case "--json":
			gf.json = true
		case "--url", "--token":
			if !hasVal {
				if i+1 >= len(args) {
					return fmt.Errorf("flag %s needs a value", name)
				}
				i++
				val = args[i]
			}
			if name == "--url" {
				gf.url = val
			} else {
				gf.token = val
			}
		default:
			return fmt.Errorf("unknown flag %q (try --help)", name)
		}
	}

	if cmd == "" {
		fmt.Print(usage)
		return nil
	}

	// Effective config: file < env < flags.
	cfg := loadConfig().mergeEnv()
	if gf.url != "" {
		cfg.URL = gf.url
	}
	if gf.token != "" {
		cfg.Token = gf.token
	}
	if gf.insecure {
		cfg.Insecure = true
	}

	if cmd == "login" {
		return cmdLogin(cfg, rest)
	}

	cl := newClient(cfg)
	switch cmd {
	case "whoami":
		return cmdWhoami(cl, gf.json)
	case "domains":
		return cmdDomains(cl, gf.json, rest)
	case "dns":
		return cmdDNS(cl, gf.json, rest)
	case "databases", "db":
		return cmdDatabases(cl, gf.json, rest)
	case "backups":
		return cmdBackups(cl, gf.json, rest)
	case "traffic":
		return cmdTraffic(cl, gf.json, rest)
	case "tokens":
		return cmdTokens(cl, gf.json, rest)
	case "api":
		return cmdAPI(cl, rest)
	default:
		return fmt.Errorf("unknown command %q (try --help)", cmd)
	}
}

// splitFlag parses "--name=value" into ("--name", "value", true) and "--name"
// into ("--name", "", false).
func splitFlag(a string) (name, val string, hasVal bool) {
	if eq := strings.IndexByte(a, '='); eq >= 0 {
		return a[:eq], a[eq+1:], true
	}
	return a, "", false
}
