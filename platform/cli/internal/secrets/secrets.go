// Package secrets reads and writes this repo's local secrets (GitHub App + PAT, `gcd creds`) as generic
// passwords in the macOS keychain, through /usr/bin/security (part of macOS: nothing to install).
package secrets

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// Service is the keychain service the items live under (the name fnox used, so items stored before
// gcd managed them keep working).
const Service = "fnox-argo-kargo"

// Names are the secrets gcd uses.
var Names = []string{"GITHUB_APP_ID", "GITHUB_APP_INSTALLATION_ID", "GITHUB_APP_PRIVATE_KEY", "GITHUB_PAT"}

var ErrNotFound = errors.New("not in the keychain")

// Keychain is the item store for one service.
type Keychain struct {
	Service string
	// run runs `security args...` and returns its stderr+stdout; tests replace it
	run func(ctx context.Context, args ...string) (string, error)
}

// Default is the keychain under Service.
func Default() Keychain { return Keychain{Service: Service, run: security} }

func security(ctx context.Context, args ...string) (string, error) {
	var out bytes.Buffer
	c := exec.CommandContext(ctx, "/usr/bin/security", args...)
	c.Stdout, c.Stderr = &out, &out
	err := c.Run()
	return out.String(), err
}

// Get returns the value of name; ErrNotFound when it was never set.
func (k Keychain) Get(ctx context.Context, name string) (string, error) {
	// -g prints the password on stderr, quoted, or as 0x<hex> when it isn't printable (e.g. a PEM key's
	// newlines); -w would print hex too, with no way to tell it from a value that looks like hex
	out, err := k.run(ctx, "find-generic-password", "-s", k.Service, "-a", name, "-g")
	v, perr := parsePassword(out)
	if errors.Is(perr, ErrNotFound) || err != nil && strings.Contains(out, "could not be found") {
		return "", fmt.Errorf("%s: %w (gcd secret set %s)", name, ErrNotFound, name)
	}
	if err != nil {
		return "", fmt.Errorf("keychain %s: %w: %s", name, err, strings.TrimSpace(out))
	}
	return v, perr
}

// Set stores value as name (replacing it). The value goes to security as hex (-X): any bytes,
// newlines included.
func (k Keychain) Set(ctx context.Context, name, value string) error {
	out, err := k.run(ctx, "add-generic-password", "-U", "-s", k.Service, "-a", name, "-X", hex.EncodeToString([]byte(value)))
	if err != nil {
		return fmt.Errorf("keychain set %s: %w: %s", name, err, strings.TrimSpace(out))
	}
	return nil
}

// parsePassword reads the `password: ...` line of `security find-generic-password -g`.
func parsePassword(out string) (string, error) {
	for _, l := range strings.Split(out, "\n") {
		v, ok := strings.CutPrefix(l, "password:")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		switch {
		case v == "":
			return "", nil
		case strings.HasPrefix(v, "0x"):
			h, _, _ := strings.Cut(v[2:], " ")
			b, err := hex.DecodeString(h)
			if err != nil {
				return "", fmt.Errorf("keychain value: %w", err)
			}
			return string(b), nil
		case len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"':
			return v[1 : len(v)-1], nil
		}
		return "", fmt.Errorf("keychain value: unexpected format")
	}
	return "", ErrNotFound
}
