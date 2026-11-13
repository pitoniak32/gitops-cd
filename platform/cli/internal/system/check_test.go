package system

import "testing"

func TestCheckName(t *testing.T) {
	for _, tc := range []struct {
		in   Check
		want string // "" = error
	}{
		{Check{File: "precheck.yaml"}, "precheck"},
		{Check{File: "security-scan.yml"}, "security-scan"},
		{Check{Name: "change-freeze", File: "freeze.yaml"}, "change-freeze"},
		{Check{File: "Scan_1.yaml"}, ""},
		{Check{Name: "promote", File: "p.yaml"}, ""},
		{Check{Name: "1st", File: "p.yaml"}, ""},
	} {
		got, err := tc.in.Normalize()
		if tc.want == "" {
			if err == nil {
				t.Errorf("%+v: want error, got %q", tc.in, got.Name)
			}
			continue
		}
		if err != nil || got.Name != tc.want {
			t.Errorf("%+v: got %q, %v; want %q", tc.in, got.Name, err, tc.want)
		}
	}
}
