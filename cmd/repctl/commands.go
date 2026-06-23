package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/reinis1996/repanel/internal/models"
)

// cmdLogin verifies the supplied credentials and saves them to the config file.
func cmdLogin(cfg Config, _ []string) error {
	if cfg.URL == "" || cfg.Token == "" {
		return fmt.Errorf("login needs both --url and --token (or REPANEL_URL/REPANEL_TOKEN)")
	}
	var u models.User
	if err := newClient(cfg).getJSON("/api/me", &u); err != nil {
		return fmt.Errorf("could not authenticate: %w", err)
	}
	if err := cfg.save(); err != nil {
		return err
	}
	fmt.Printf("Logged in as %s (%s). Config saved to %s\n", u.Username, u.Role, configPath())
	return nil
}

func cmdWhoami(cl *Client, asJSON bool) error {
	var u models.User
	if err := cl.getJSON("/api/me", &u); err != nil {
		return err
	}
	if asJSON {
		return printJSON(u)
	}
	fmt.Printf("%s <%s>  role=%s  id=%d\n", u.Username, u.Email, u.Role, u.ID)
	return nil
}

func cmdDomains(cl *Client, asJSON bool, args []string) error {
	sub, rest := shift(args)
	switch sub {
	case "", "list":
		var ds []models.Domain
		if err := cl.getJSON("/api/domains", &ds); err != nil {
			return err
		}
		if asJSON {
			return printJSON(ds)
		}
		w := newTable("ID", "DOMAIN", "PHP", "SSL", "STATUS", "OWNER")
		for _, d := range ds {
			fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s\n", d.ID, d.Name, d.PHPVersion,
				yesNo(d.SSL), suspendedLabel(d.Suspended), d.Owner)
		}
		return w.Flush()
	case "create":
		name, php, dns := "", "", false
		for i := 0; i < len(rest); i++ {
			switch n, v, has := splitFlag(rest[i]); n {
			case "--php":
				if !has {
					i++
					if i < len(rest) {
						v = rest[i]
					}
				}
				php = v
			case "--dns":
				dns = true
			default:
				if !strings.HasPrefix(rest[i], "-") && name == "" {
					name = rest[i]
				}
			}
		}
		if name == "" {
			return fmt.Errorf("usage: repctl domains create NAME [--php VERSION] [--dns]")
		}
		var out models.Domain
		if err := cl.postJSON("/api/domains", map[string]any{
			"name": name, "php_version": php, "create_dns": dns,
		}, &out); err != nil {
			return err
		}
		fmt.Printf("Created domain %s (id %d)\n", out.Name, out.ID)
		return nil
	case "delete", "rm":
		id, err := wantID(rest, "domains delete ID")
		if err != nil {
			return err
		}
		if _, err := cl.do("DELETE", "/api/domains/"+id, nil); err != nil {
			return err
		}
		fmt.Println("Domain deleted")
		return nil
	default:
		return fmt.Errorf("unknown domains subcommand %q", sub)
	}
}

func cmdDNS(cl *Client, asJSON bool, args []string) error {
	sub, _ := shift(args)
	switch sub {
	case "", "list":
		var zs []models.DNSZone
		if err := cl.getJSON("/api/dns", &zs); err != nil {
			return err
		}
		if asJSON {
			return printJSON(zs)
		}
		w := newTable("ID", "ZONE", "SERIAL")
		for _, z := range zs {
			fmt.Fprintf(w, "%d\t%s\t%d\n", z.ID, z.Name, z.Serial)
		}
		return w.Flush()
	default:
		return fmt.Errorf("unknown dns subcommand %q", sub)
	}
}

func cmdDatabases(cl *Client, asJSON bool, args []string) error {
	sub, _ := shift(args)
	switch sub {
	case "", "list":
		var dbs []models.DatabaseEntry
		if err := cl.getJSON("/api/databases", &dbs); err != nil {
			return err
		}
		if asJSON {
			return printJSON(dbs)
		}
		w := newTable("ID", "NAME", "ENGINE", "USER", "SIZE")
		for _, d := range dbs {
			fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%.1f MB\n", d.ID, d.Name, dbEngineLabel(d.Engine), d.DBUser, d.SizeMB)
		}
		return w.Flush()
	default:
		return fmt.Errorf("unknown databases subcommand %q", sub)
	}
}

func cmdBackups(cl *Client, asJSON bool, args []string) error {
	sub, _ := shift(args)
	switch sub {
	case "", "list":
		var bs []models.Backup
		if err := cl.getJSON("/api/backups", &bs); err != nil {
			return err
		}
		if asJSON {
			return printJSON(bs)
		}
		w := newTable("ID", "CREATED", "ACCOUNT", "SIZE", "STATUS")
		for _, b := range bs {
			fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\n", b.ID,
				b.CreatedAt.Format("2006-01-02 15:04"), b.Owner, humanBytes(b.SizeBytes), b.Status)
		}
		return w.Flush()
	case "create":
		if err := cl.postJSON("/api/backups", map[string]any{}, nil); err != nil {
			return err
		}
		fmt.Println("Backup started")
		return nil
	default:
		return fmt.Errorf("unknown backups subcommand %q", sub)
	}
}

func cmdTraffic(cl *Client, asJSON bool, args []string) error {
	sub, _ := shift(args)
	switch sub {
	case "", "list":
		var ts []models.TrafficStat
		if err := cl.getJSON("/api/traffic", &ts); err != nil {
			return err
		}
		if asJSON {
			return printJSON(ts)
		}
		w := newTable("ACCOUNT", "BANDWIDTH (30d)", "DOMAINS")
		for _, t := range ts {
			fmt.Fprintf(w, "%s\t%.1f MB\t%d\n", t.Username, t.TotalMB, len(t.Domains))
		}
		return w.Flush()
	default:
		return fmt.Errorf("unknown traffic subcommand %q", sub)
	}
}

func cmdTokens(cl *Client, asJSON bool, args []string) error {
	sub, _ := shift(args)
	switch sub {
	case "", "list":
		var ts []models.APIToken
		if err := cl.getJSON("/api/tokens", &ts); err != nil {
			return err
		}
		if asJSON {
			return printJSON(ts)
		}
		w := newTable("ID", "NAME", "PREFIX", "LAST USED", "EXPIRES")
		for _, t := range ts {
			fmt.Fprintf(w, "%d\t%s\t%s…\t%s\t%s\n", t.ID, t.Name, t.Prefix,
				ptime(t.LastUsedAt), ptime(t.ExpiresAt))
		}
		return w.Flush()
	default:
		return fmt.Errorf("unknown tokens subcommand %q", sub)
	}
}

// cmdAPI is a raw passthrough: `api METHOD PATH [JSON-BODY]`. It prints the
// server's response verbatim (pretty-printed if it is JSON).
func cmdAPI(cl *Client, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: repctl api METHOD PATH [JSON-BODY]")
	}
	method := strings.ToUpper(args[0])
	path := args[1]
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	var body []byte
	if len(args) >= 3 {
		body = []byte(args[2])
	}
	data, err := cl.do(method, path, body)
	if err != nil {
		return err
	}
	var pretty any
	if json.Unmarshal(data, &pretty) == nil {
		return printJSON(pretty)
	}
	_, err = os.Stdout.Write(data)
	return err
}

// ---- small helpers ----

func shift(args []string) (string, []string) {
	if len(args) == 0 {
		return "", nil
	}
	return args[0], args[1:]
}

func wantID(args []string, usage string) (string, error) {
	for _, a := range args {
		if _, err := strconv.Atoi(a); err == nil {
			return a, nil
		}
	}
	return "", fmt.Errorf("usage: repctl %s", usage)
}

func newTable(headers ...string) *tabwriter.Writer {
	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, strings.Join(headers, "\t"))
	return w
}

func printJSON(v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(b))
	return nil
}

func dbEngineLabel(e string) string {
	if e == "postgres" {
		return "PostgreSQL"
	}
	return "MariaDB"
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func suspendedLabel(b bool) string {
	if b {
		return "suspended"
	}
	return "active"
}

func humanBytes(n int64) string {
	const u = 1024
	if n < u {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(u), 0
	for x := n / u; x >= u; x /= u {
		div *= u
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGT"[exp])
}

// ptime formats a nullable timestamp, showing "never" when unset.
func ptime(t *time.Time) string {
	if t == nil || t.IsZero() {
		return "never"
	}
	return t.Local().Format("2006-01-02 15:04")
}
