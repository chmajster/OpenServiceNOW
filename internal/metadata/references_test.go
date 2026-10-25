package metadata

import (
	"strings"
	"testing"
)

func TestRequestReferencesUseRequesterOrAssignedGroupAccess(t *testing.T) {
	actor := Actor{UserID: 42, Permissions: map[string]bool{"request.fulfill": true}}
	predicate, args, ok := requestReferencePredicate(actor)
	if !ok {
		t.Fatal("fulfillment actor should receive an object-scoped reference predicate")
	}
	for _, fragment := range []string{"fulfillment_tasks", "work_assignments", "group_memberships", "gm.user_id=?"} {
		if !strings.Contains(predicate, fragment) {
			t.Fatalf("fulfillment predicate %q does not include %q", predicate, fragment)
		}
	}
	if strings.Contains(predicate, "1=1") || strings.Contains(predicate, "r.requester_id") {
		t.Fatalf("fulfillment access must be limited to assigned task groups: %q", predicate)
	}
	if len(args) != 1 || args[0] != uint64(42) {
		t.Fatalf("fulfillment predicate args = %#v", args)
	}
	if canListAllGroups(actor) || canListTenantUsers(actor) {
		t.Fatal("request.fulfill must not grant tenant-wide group or user directory access")
	}
	if !canReferenceGroups(actor) {
		t.Fatal("fulfillment users must still be able to look up groups they belong to")
	}

	requester := Actor{UserID: 7, Permissions: map[string]bool{"request.read_own": true}}
	predicate, args, ok = requestReferencePredicate(requester)
	if !ok || predicate != "(r.requester_id=?)" || len(args) != 1 || args[0] != uint64(7) {
		t.Fatalf("requester predicate=%q args=%#v ok=%t", predicate, args, ok)
	}
	if _, _, ok = requestReferencePredicate(Actor{}); ok {
		t.Fatal("actor without request visibility must not receive a reference predicate")
	}
}
