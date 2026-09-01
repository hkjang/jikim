package store

import (
	"testing"

	"github.com/hkjang/jikim/internal/model"
)

func TestPersonalKeyDecryptPermissionCannotBeRemoved(t *testing.T) {
	if _, err := cleanUserKeyPermissions(map[string]any{"encrypt": true, "rotate": true}); err == nil {
		t.Fatal("missing decrypt permission was accepted")
	}
	if _, err := cleanUserKeyPermissions(map[string]any{"decrypt": false}); err == nil {
		t.Fatal("disabled decrypt permission was accepted")
	}
	if permissions, err := cleanUserKeyPermissions(map[string]any{"decrypt": true, "rotate": false}); err != nil || !permissions["decrypt"] {
		t.Fatalf("valid permissions rejected: %#v, %v", permissions, err)
	}
	if _, err := cleanUserKeyPermissions(map[string]any{"decrypt": true, "manage": true}); err == nil {
		t.Fatal("transit-only manage permission was accepted for a personal key")
	}
}

func TestActiveAdminInvariant(t *testing.T) {
	admin := model.User{Role: "admin", Active: true}
	if err := validateActiveAdminInvariant(admin, "user", true, 1); err == nil {
		t.Fatal("last active admin role downgrade accepted")
	}
	if err := validateActiveAdminInvariant(admin, "admin", false, 1); err == nil {
		t.Fatal("last active admin deactivation accepted")
	}
	if err := validateActiveAdminInvariant(admin, "user", true, 2); err != nil {
		t.Fatalf("admin downgrade with another active admin rejected: %v", err)
	}
}

func TestAdminCannotMutateSelfThroughUserManagement(t *testing.T) {
	if err := validateAdminActor("same", "same"); err == nil {
		t.Fatal("admin self-management mutation accepted")
	}
	if err := validateAdminActor("target", "actor"); err != nil {
		t.Fatalf("different target rejected: %v", err)
	}
}
