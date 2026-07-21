package sessions

import (
	"os"
	"path/filepath"
	"testing"
)

func writePasswd(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "passwd")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestListUsersFiltersSystemAndServiceAccounts(t *testing.T) {
	p := writePasswd(t, ""+
		"root:x:0:0:root:/root:/bin/bash\n"+
		"hush:x:998:998::/nonexistent:/usr/sbin/nologin\n"+ // useradd --system account, like hush's own
		"nobody:x:65534:65534:nobody:/nonexistent:/usr/sbin/nologin\n"+
		"ci:x:1001:1001::/home/ci:/bin/false\n"+ // human uid but a non-login shell
		"josh:x:1000:1000:Josh:/home/josh:/bin/bash\n"+
		"maya:x:1002:1002:Maya:/home/maya:/bin/zsh\n")

	got := ListUsers(p)
	if len(got) != 2 {
		t.Fatalf("ListUsers() = %+v, want 2 human accounts", got)
	}
	// Sorted by name.
	if got[0].Name != "josh" || got[0].UID != 1000 || got[0].Home != "/home/josh" || got[0].Shell != "/bin/bash" {
		t.Fatalf("unexpected users[0]: %+v", got[0])
	}
	if got[1].Name != "maya" || got[1].UID != 1002 {
		t.Fatalf("unexpected users[1]: %+v", got[1])
	}
}

func TestListUsersSkipsBlankAndMalformedLines(t *testing.T) {
	p := writePasswd(t, ""+
		"\n"+
		"# a comment\n"+
		"broken:x:1000\n"+ // too few fields
		"notanumber:x:abc:1000::/home/notanumber:/bin/bash\n"+
		"josh:x:1000:1000:Josh:/home/josh:/bin/bash\n")

	got := ListUsers(p)
	if len(got) != 1 || got[0].Name != "josh" {
		t.Fatalf("ListUsers() = %+v, want just josh", got)
	}
}

func TestListUsersMissingFile(t *testing.T) {
	got := ListUsers(filepath.Join(t.TempDir(), "does-not-exist"))
	if got != nil {
		t.Fatalf("ListUsers() = %+v, want nil for a missing file", got)
	}
}

func TestCollectUsersSetsHost(t *testing.T) {
	snap := CollectUsers()
	host, _ := os.Hostname()
	if snap.Host != host {
		t.Fatalf("CollectUsers().Host = %q, want %q", snap.Host, host)
	}
}
