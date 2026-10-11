package tool_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/lassoColombo/agent-notify/tool"
)

// TestTheToolIsWhereTheSettingSaysOrOnPATH is D-101: the key is the override,
// PATH is the ordinary answer, and failing both names the key.
func TestTheToolIsWhereTheSettingSaysOrOnPATH(t *testing.T) {
	if found, err := tool.Find("zellij", "sh", ""); err != nil || filepath.Base(found) != "sh" {
		t.Errorf("Find on PATH = %q, %v; want sh", found, err)
	}
	if found, err := tool.Find("zellij", "sh", "/bin/sh"); err != nil || found != "/bin/sh" {
		t.Errorf("Find configured = %q, %v; want it taken as written", found, err)
	}
	if _, err := tool.Find("[integration.zellij.settings] zellij", "sh", "/nowhere/at/all/sh"); err == nil ||
		!strings.Contains(err.Error(), "no such file") {
		t.Errorf("a configured path that is not there = %v; want it refused", err)
	}

	t.Setenv("PATH", t.TempDir())
	_, err := tool.Find("[integration.zellij.settings] zellij", "sh", "")
	if err == nil || !strings.Contains(err.Error(), "[integration.zellij.settings] zellij") {
		t.Errorf("not on PATH = %v; want the key named", err)
	}
}
