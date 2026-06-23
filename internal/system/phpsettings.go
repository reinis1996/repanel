package system

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/reinis1996/repanel/internal/models"
)

// This file renders the structured, owner-editable per-domain PHP settings
// (models.PHPSettings, stored as JSON on the domain) into the php_admin_value
// lines injected into the domain's PHP-FPM pool. Unlike the admin raw php_conf
// block, these are validated, bounded values a site owner may safely set.

// phpSizeRe accepts a PHP "shorthand byte" value: digits with an optional
// K/M/G suffix. Anything else is rejected so a customer can't smuggle arbitrary
// pool directives through a settings field.
var phpSizeRe = regexp.MustCompile(`^\d+[KMG]?$`)

// phpFuncRe accepts a comma-separated list of PHP function identifiers.
var phpFuncListRe = regexp.MustCompile(`^[A-Za-z0-9_,]*$`)

// ParsePHPSettings decodes the stored JSON, returning the zero value on empty
// or malformed input.
func ParsePHPSettings(raw string) models.PHPSettings {
	var s models.PHPSettings
	if strings.TrimSpace(raw) == "" {
		return s
	}
	_ = json.Unmarshal([]byte(raw), &s)
	return s
}

// SanitizePHPSettings clamps/validates a settings struct to safe values and
// returns it together with the marshaled JSON to persist. Invalid size/number
// fields are dropped (left at the pool default) rather than rejected, so the
// form is forgiving.
func SanitizePHPSettings(s models.PHPSettings) (models.PHPSettings, string) {
	clean := models.PHPSettings{
		MemoryLimit:       cleanSize(s.MemoryLimit),
		UploadMaxFilesize: cleanSize(s.UploadMaxFilesize),
		PostMaxSize:       cleanSize(s.PostMaxSize),
		MaxExecutionTime:  cleanInt(s.MaxExecutionTime, 0, 3600),
		MaxInputTime:      cleanInt(s.MaxInputTime, -1, 3600),
		MaxInputVars:      cleanInt(s.MaxInputVars, 0, 100000),
		DisplayErrors:     s.DisplayErrors,
		AllowUrlFopen:     s.AllowUrlFopen,
		DisableFunctions:  cleanFuncList(s.DisableFunctions),
	}
	b, _ := json.Marshal(clean)
	return clean, string(b)
}

func cleanSize(v string) string {
	v = strings.ToUpper(strings.TrimSpace(v))
	if v == "" || !phpSizeRe.MatchString(v) {
		return ""
	}
	return v
}

func cleanInt(v string, min, max int) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < min || n > max {
		return ""
	}
	return strconv.Itoa(n)
}

func cleanFuncList(v string) string {
	v = strings.ReplaceAll(strings.TrimSpace(v), " ", "")
	if v == "" || !phpFuncListRe.MatchString(v) {
		return ""
	}
	return v
}

// RenderPHPSettings turns the stored JSON settings into php_admin_value pool
// lines, applying the panel defaults for upload/post size when unset so the
// pool keeps its historical 128M ceiling unless the owner overrides it. When no
// settings have ever been saved (blank JSON) it emits only the historical size
// defaults, so an untouched domain behaves exactly as before — in particular it
// must not force allow_url_fopen/display_errors to a non-default value.
func RenderPHPSettings(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return "php_admin_value[upload_max_filesize] = 128M\nphp_admin_value[post_max_size] = 128M"
	}
	s := ParsePHPSettings(raw)
	upload := s.UploadMaxFilesize
	if upload == "" {
		upload = "128M"
	}
	post := s.PostMaxSize
	if post == "" {
		post = "128M"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "php_admin_value[upload_max_filesize] = %s\n", upload)
	fmt.Fprintf(&b, "php_admin_value[post_max_size] = %s\n", post)
	if s.MemoryLimit != "" {
		fmt.Fprintf(&b, "php_admin_value[memory_limit] = %s\n", s.MemoryLimit)
	}
	if s.MaxExecutionTime != "" {
		fmt.Fprintf(&b, "php_admin_value[max_execution_time] = %s\n", s.MaxExecutionTime)
	}
	if s.MaxInputTime != "" {
		fmt.Fprintf(&b, "php_admin_value[max_input_time] = %s\n", s.MaxInputTime)
	}
	if s.MaxInputVars != "" {
		fmt.Fprintf(&b, "php_admin_value[max_input_vars] = %s\n", s.MaxInputVars)
	}
	fmt.Fprintf(&b, "php_admin_flag[display_errors] = %s\n", onOff(s.DisplayErrors))
	fmt.Fprintf(&b, "php_admin_flag[allow_url_fopen] = %s\n", onOff(s.AllowUrlFopen))
	if s.DisableFunctions != "" {
		fmt.Fprintf(&b, "php_admin_value[disable_functions] = %s", s.DisableFunctions)
	}
	return strings.TrimRight(b.String(), "\n")
}

func onOff(b bool) string {
	if b {
		return "On"
	}
	return "Off"
}
