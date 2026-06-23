package system

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/reinis1996/repanel/internal/models"
)

const cronFile = "/etc/cron.d/repanel"

// cron.d entries require a user field; jobs run as the owning system user.
// Fields are separated by spaces only — using a literal space (not \s, which
// matches newlines) prevents a crafted schedule from injecting extra cron.d
// lines (see SECURITY_AUDIT F-12).
var cronScheduleRe = regexp.MustCompile(`^(@(reboot|yearly|annually|monthly|weekly|daily|hourly)|(\S+ +){4}\S+)$`)

// ValidateCronSchedule loosely validates a 5-field or @keyword schedule.
func ValidateCronSchedule(s string) error {
	s = strings.TrimSpace(s)
	if strings.ContainsAny(s, "\n\r\t") || !cronScheduleRe.MatchString(s) {
		return fmt.Errorf("invalid cron schedule %q (expected 5 fields or @daily style)", s)
	}
	return nil
}

// RebuildCrontab regenerates /etc/cron.d/repanel from all enabled jobs.
// sysUserFor maps a panel user id to its unix account.
func RebuildCrontab(jobs []models.CronJob, sysUserFor func(int64) string) error {
	if !Linux() {
		return nil
	}
	var sb strings.Builder
	sb.WriteString("# Managed by RePanel — do not edit.\nSHELL=/bin/sh\nPATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin\n\n")
	for _, j := range jobs {
		if !j.Enabled {
			continue
		}
		user := sysUserFor(j.UserID)
		if user == "" {
			continue
		}
		if j.Comment != "" {
			fmt.Fprintf(&sb, "# %s\n", strings.ReplaceAll(j.Comment, "\n", " "))
		}
		// Strip newlines so a crafted command cannot inject extra cron lines.
		command := strings.ReplaceAll(strings.ReplaceAll(j.Command, "\n", " "), "\r", " ")
		fmt.Fprintf(&sb, "%s %s %s\n", j.Schedule, user, command)
	}
	return os.WriteFile(cronFile, []byte(sb.String()), 0o644)
}
