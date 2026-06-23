//go:build !linux

package system

// oNoFollow is a no-op off Linux (Windows dev hosts), where O_NOFOLLOW is not
// defined and the file-manager integrations are not exercised against untrusted
// tenants.
const oNoFollow = 0
