//go:build windows

package appscan

import (
	"reflect"
	"testing"
)

func TestSplitArgsQuotedValues(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want []string
	}{
		{name: "empty", raw: "", want: nil},
		{name: "spaces", raw: "   \t ", want: nil},
		{name: "single flag", raw: "--verbose", want: []string{"--verbose"}},
		{name: "quoted profile", raw: `--profile "My Profile"`, want: []string{"--profile", "My Profile"}},
		{
			name: "quoted executable and args",
			raw:  `"C:\Program Files\App\app.exe" --x "a b"`,
			want: []string{`C:\Program Files\App\app.exe`, "--x", "a b"},
		},
		{name: "empty quoted argument", raw: `""`, want: []string{""}},
		{name: "escaped quote", raw: `"a\"b"`, want: []string{`a"b`}},
		{name: "doubled quote", raw: `"a""b"`, want: []string{`a"b`}},
		{name: "backslashes are literal", raw: `--path C:\\server\\share`, want: []string{"--path", `C:\\server\\share`}},
		{name: "single quoted argument with equals", raw: `--url="https://example.com/a b"`, want: []string{"--url=https://example.com/a b"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := splitArgs(test.raw)
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("splitArgs(%q) = %#v, want %#v", test.raw, got, test.want)
			}
		})
	}
}
