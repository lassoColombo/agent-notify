package tool_test

import (
	"strings"
	"testing"

	"github.com/lassoColombo/agent-notify/tool"
)

func TestTheToolMustBeNamedAndAbsolute(t *testing.T) {
	for _, one := range []struct{ given, mention string }{
		{"", "is not set"},
		{"zellij", "absolute"},
		{"./zellij", "absolute"},
		{"/nowhere/at/all/zellij", "no such file"},
	} {
		_, err := tool.AbsolutePath("zellij", one.given)
		if err == nil || !strings.Contains(err.Error(), one.mention) {
			t.Errorf("AbsolutePath(%q) = %v, want it to mention %q", one.given, err, one.mention)
		}
	}
	if _, err := tool.AbsolutePath("sh", "/bin/sh"); err != nil {
		t.Error(err)
	}
}
