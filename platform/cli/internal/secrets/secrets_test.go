package secrets

import (
	"context"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

func TestParsePassword(t *testing.T) {
	pem := "-----BEGIN KEY-----\nabc\n-----END KEY-----\n"
	for _, c := range []struct{ in, want string }{
		{`password: "123456"` + "\n", "123456"},
		{"password: 0x" + strings.ToUpper(hex.EncodeToString([]byte(pem))) + `  "-----BEGIN KEY-----\012abc..."` + "\n", pem},
		{"password: \n", ""},
		{`password: "with "quotes" inside"` + "\n", `with "quotes" inside`},
	} {
		got, err := parsePassword(c.in)
		if err != nil || got != c.want {
			t.Errorf("%q: got %q err %v", c.in, got, err)
		}
	}
	if _, err := parsePassword("security: SecKeychainSearchCopyNext: The specified item could not be found in the keychain.\n"); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing item: %v", err)
	}
}

func TestGetSet(t *testing.T) {
	store := map[string]string{}
	k := Keychain{Service: "svc", run: func(_ context.Context, args ...string) (string, error) {
		acct := args[indexOf(args, "-a")+1]
		switch args[0] {
		case "add-generic-password":
			b, _ := hex.DecodeString(args[indexOf(args, "-X")+1])
			store[acct] = string(b)
			return "", nil
		case "find-generic-password":
			v, ok := store[acct]
			if !ok {
				return "security: The specified item could not be found in the keychain.\n", errors.New("exit status 44")
			}
			return "password: 0x" + hex.EncodeToString([]byte(v)) + "  \"x\"\n", nil
		}
		return "", errors.New("unexpected")
	}}
	ctx := context.Background()
	if _, err := k.Get(ctx, "A"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
	if err := k.Set(ctx, "A", "line1\nline2"); err != nil {
		t.Fatal(err)
	}
	if v, err := k.Get(ctx, "A"); err != nil || v != "line1\nline2" {
		t.Fatalf("got %q %v", v, err)
	}
}

func indexOf(l []string, s string) int {
	for i, x := range l {
		if x == s {
			return i
		}
	}
	return -1
}
