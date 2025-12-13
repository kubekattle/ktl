package buildkit

import (
	"errors"
	"os"
	"syscall"
	"testing"
)

func TestIsDialError(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "grpc unix missing",
			err:  errors.New("transport: Error while dialing: dial unix /run/user/0/buildkit/buildkitd.sock: connect: no such file or directory"),
			want: true,
		},
		{
			name: "wrapped econ refused",
			err:  &os.SyscallError{Syscall: "connect", Err: syscall.ECONNREFUSED},
			want: true,
		},
		{
			name: "generic error",
			err:  errors.New("some other failure"),
			want: false,
		},
		{
			name: "nil",
			err:  nil,
			want: false,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := isDialError(tc.err); got != tc.want {
				t.Fatalf("isDialError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
