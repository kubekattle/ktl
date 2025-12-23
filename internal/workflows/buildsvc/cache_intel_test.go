package buildsvc

import "testing"

func TestParseDockerfileCopyAddSources(t *testing.T) {
	input := `
# comment
FROM alpine
COPY --from=builder --chown=0:0 foo bar /dest/
ADD ["a.txt", "b.txt", "/dst/"]
COPY one \\
  two /dst/
RUN echo hi
`
	got := parseDockerfileCopyAddSources(input)
	want := []string{"foo", "bar", "a.txt", "b.txt", "one", "two"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestParseDockerfileRefs_DetectsBroadCopyAndMounts(t *testing.T) {
	input := `
FROM alpine
COPY . /src
RUN --mount=type=secret,id=foo echo hi
RUN --mount=type=ssh echo hi
`
	_, broad, secrets, ssh := parseDockerfileRefs(input)
	if !broad {
		t.Fatalf("expected broad copy")
	}
	if secrets != 1 {
		t.Fatalf("expected 1 secret mount, got %d", secrets)
	}
	if ssh != 1 {
		t.Fatalf("expected 1 ssh mount, got %d", ssh)
	}
}

func TestDiffCacheIntelInputs(t *testing.T) {
	prev := &cacheIntelInputsSnapshot{
		Version:         1,
		DockerfileSHA:   "a",
		DockerignoreSHA: "x",
		BuildArgSHA:     map[string]string{"A": "1", "B": "2"},
		SecretIDs:       []string{"S1"},
		FileSHA:         map[string]string{"foo": "aa", "bar": "bb"},
	}
	cur := &cacheIntelInputsSnapshot{
		Version:         1,
		DockerfileSHA:   "b",
		DockerignoreSHA: "x",
		BuildArgSHA:     map[string]string{"A": "1", "B": "3", "C": "4"},
		SecretIDs:       []string{"S2"},
		FileSHA:         map[string]string{"foo": "aa", "bar": "cc"},
	}

	diff := diffCacheIntelInputs(prev, cur, 10)
	if !diff.DockerfileChanged {
		t.Fatalf("expected DockerfileChanged")
	}
	if diff.DockerignoreChanged {
		t.Fatalf("did not expect DockerignoreChanged")
	}
	if !diff.SecretsChanged {
		t.Fatalf("expected SecretsChanged")
	}
	if len(diff.BuildArgsChanged) != 2 {
		t.Fatalf("expected 2 build arg changes, got %d: %v", len(diff.BuildArgsChanged), diff.BuildArgsChanged)
	}
	if len(diff.FilesChanged) != 1 || diff.FilesChanged[0].Key != "bar" {
		t.Fatalf("expected bar to change, got %v", diff.FilesChanged)
	}
}
