package stack

import "testing"

func TestClassifyError(t *testing.T) {
	cases := []struct {
		msg  string
		want string
	}{
		{"429 Too Many Requests", "RATE_LIMIT"},
		{"context deadline exceeded", "TIMEOUT"},
		{"connection reset by peer", "TRANSPORT"},
		{"server error 500", "SERVER_5XX"},
		{"forbidden", "OTHER"},
	}
	for _, tc := range cases {
		got := classifyError(errString(tc.msg))
		if got != tc.want {
			t.Fatalf("classify(%q)=%q want=%q", tc.msg, got, tc.want)
		}
	}
}

type errString string

func (e errString) Error() string { return string(e) }
