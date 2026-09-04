package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/hkjang/jikim/internal/cryptox"
	"github.com/hkjang/jikim/internal/ids"
	"github.com/hkjang/jikim/internal/model"
	"github.com/jackc/pgx/v5/pgconn"
)

func intPointer(value int) *int { return &value }

func TestValidateOpenBaoCAS(t *testing.T) {
	tests := []struct {
		name           string
		cas            *int
		currentVersion int
		exists         bool
		want           error
	}{
		{name: "unset on new key", cas: nil, exists: false},
		{name: "unset on existing key", cas: nil, currentVersion: 4, exists: true},
		{name: "zero creates new key", cas: intPointer(0), exists: false},
		{name: "exact version updates", cas: intPointer(4), currentVersion: 4, exists: true},
		{name: "zero cannot overwrite", cas: intPointer(0), currentVersion: 1, exists: true, want: ErrCheckAndSet},
		{name: "stale version rejected", cas: intPointer(3), currentVersion: 4, exists: true, want: ErrCheckAndSet},
		{name: "positive version cannot create", cas: intPointer(1), exists: false, want: ErrCheckAndSet},
		{name: "negative does not match", cas: intPointer(-1), exists: false, want: ErrCheckAndSet},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateOpenBaoCAS(test.cas, test.currentVersion, test.exists)
			if test.want == nil && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if test.want != nil && !errors.Is(err, test.want) {
				t.Fatalf("error=%v, want %v", err, test.want)
			}
		})
	}
}

func TestNormalizeOpenBaoVersions(t *testing.T) {
	got, err := normalizeOpenBaoVersions([]int{3, 1, 3, 2})
	if err != nil {
		t.Fatal(err)
	}
	want := []int{1, 2, 3}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("versions=%v, want %v", got, want)
		}
	}
	for _, invalid := range [][]int{nil, {}} {
		if _, err := normalizeOpenBaoVersions(invalid); !errors.Is(err, ErrInvalid) {
			t.Fatalf("versions=%v error=%v, want ErrInvalid", invalid, err)
		}
	}
	got, err = normalizeOpenBaoVersions([]int{0, -1, 2})
	if err != nil || len(got) != 1 || got[0] != 2 {
		t.Fatalf("non-positive OpenBao versions were not ignored: versions=%v error=%v", got, err)
	}
}

func TestOpenBaoCASStorageConflict(t *testing.T) {
	cas := intPointer(0)
	for _, code := range []string{"23505", "40001"} {
		if !openBaoCASStorageConflict(cas, &pgconn.PgError{Code: code}) {
			t.Fatalf("PostgreSQL conflict %s was not mapped to CAS mismatch", code)
		}
	}
	if openBaoCASStorageConflict(nil, &pgconn.PgError{Code: "23505"}) {
		t.Fatal("write without CAS was mapped to CAS mismatch")
	}
	if openBaoCASStorageConflict(cas, &pgconn.PgError{Code: "23503"}) {
		t.Fatal("unrelated PostgreSQL error was mapped to CAS mismatch")
	}
	if !isPostgresSerialization(&pgconn.PgError{Code: "40001"}) ||
		isPostgresSerialization(&pgconn.PgError{Code: "23505"}) {
		t.Fatal("PostgreSQL serialization detection is incorrect")
	}
}

// This test is opt-in because the normal unit-test contract has no PostgreSQL
// dependency. Point JIKIM_TEST_POSTGRES_DSN at a disposable database to verify
// the transaction and migration behavior end to end.
func TestOpenBaoKVLifecycleIntegration(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("JIKIM_TEST_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip("JIKIM_TEST_POSTGRES_DSN is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	master, err := cryptox.New(bytes.Repeat([]byte{0x4b}, cryptox.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	st, err := New(ctx, dsn, master)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := st.VerifyMasterKey(ctx); err != nil {
		t.Fatal(err)
	}
	suffix, err := ids.UUID()
	if err != nil {
		t.Fatal(err)
	}
	suffix = strings.ReplaceAll(suffix, "-", "")
	admin, _, err := st.BootstrapAdmin(ctx, "kvtest-"+suffix, "OpenBao-KV-Test-Password-2026")
	if err != nil {
		t.Fatal(err)
	}
	path := "kvtest/" + suffix
	ownerID := admin.ID
	created, err := st.PutSecret(ctx, model.SecretWrite{
		Path: path, Description: "preserve me", OwnerUserID: &ownerID, Tags: []string{"critical"},
		Metadata: map[string]any{"environment": "PRD", "owner": admin.Username},
		Data:     map[string]any{"value": "one"},
	}, admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	emptyPath := path + "/empty"
	if _, err := st.PutOpenBaoSecret(ctx, emptyPath, map[string]any{}, admin.ID, intPointer(0)); err != nil {
		t.Fatalf("OpenBao empty data map was rejected: %v", err)
	}
	if _, err := st.PutOpenBaoSecret(ctx, path, map[string]any{"value": "bad"}, admin.ID, intPointer(0)); !errors.Is(err, ErrCheckAndSet) {
		t.Fatalf("cas=0 overwrite error=%v", err)
	}
	updated, err := st.PutOpenBaoSecret(ctx, path, map[string]any{"value": "two"}, admin.ID, intPointer(created.Version))
	if err != nil {
		t.Fatal(err)
	}
	if updated.Description != "preserve me" || updated.OwnerUserID == nil || *updated.OwnerUserID != admin.ID || len(updated.Tags) != 1 {
		t.Fatalf("jikim management metadata was replaced: %#v", updated)
	}
	if updated.Metadata["environment"] != "PRD" {
		t.Fatalf("version metadata was not preserved: %#v", updated.Metadata)
	}
	type casResult struct {
		secret model.Secret
		err    error
	}
	casVersion := updated.Version
	start := make(chan struct{})
	results := make(chan casResult, 2)
	for index := 0; index < 2; index++ {
		go func(candidate int) {
			<-start
			secret, writeErr := st.PutOpenBaoSecret(ctx, path,
				map[string]any{"value": candidate}, admin.ID, intPointer(casVersion))
			results <- casResult{secret: secret, err: writeErr}
		}(index)
	}
	close(start)
	successes, mismatches := 0, 0
	for index := 0; index < 2; index++ {
		result := <-results
		switch {
		case result.err == nil:
			successes++
			updated = result.secret
		case errors.Is(result.err, ErrCheckAndSet):
			mismatches++
		default:
			t.Fatalf("concurrent CAS error=%v", result.err)
		}
	}
	if successes != 1 || mismatches != 1 {
		t.Fatalf("concurrent CAS successes=%d mismatches=%d", successes, mismatches)
	}
	start = make(chan struct{})
	results = make(chan casResult, 2)
	for index := 0; index < 2; index++ {
		go func(candidate int) {
			<-start
			secret, writeErr := st.PutOpenBaoSecret(ctx, path,
				map[string]any{"unconditional": candidate}, admin.ID, nil)
			results <- casResult{secret: secret, err: writeErr}
		}(index)
	}
	close(start)
	seenVersions := map[int]bool{}
	for index := 0; index < 2; index++ {
		result := <-results
		if result.err != nil {
			t.Fatalf("concurrent write without CAS error=%v", result.err)
		}
		seenVersions[result.secret.Version] = true
		if result.secret.Version > updated.Version {
			updated = result.secret
		}
	}
	if len(seenVersions) != 2 {
		t.Fatalf("concurrent writes did not create distinct versions: %v", seenVersions)
	}
	if err := st.SoftDeleteLatestOpenBaoVersion(ctx, path); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetSecret(ctx, path, updated.Version); !errors.Is(err, ErrNotFound) {
		t.Fatalf("soft-deleted version remained readable: %v", err)
	}
	metadata, err := st.OpenBaoKVMetadata(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	latest, ok := integrationVersion(metadata, updated.Version)
	if !ok || latest.DeletionTime == nil || latest.Destroyed {
		t.Fatalf("invalid soft-delete metadata: %#v", latest)
	}
	if err := st.UndeleteOpenBaoVersions(ctx, path, []int{updated.Version}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetSecret(ctx, path, updated.Version); err != nil {
		t.Fatalf("undeleted version is unreadable: %v", err)
	}
	if err := st.DestroySecretVersions(ctx, path, []int{created.Version}); err != nil {
		t.Fatal(err)
	}
	metadata, err = st.OpenBaoKVMetadata(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	destroyed, ok := integrationVersion(metadata, created.Version)
	if !ok || !destroyed.Destroyed || destroyed.DeletionTime != nil {
		t.Fatalf("invalid destroyed metadata: %#v", destroyed)
	}
	if err := st.DeleteOpenBaoMetadata(ctx, path); err != nil {
		t.Fatal(err)
	}
	if _, err := st.OpenBaoKVMetadata(ctx, path); !errors.Is(err, ErrNotFound) {
		t.Fatalf("metadata delete left key behind: %v", err)
	}

	userID, err := ids.UUID()
	if err != nil {
		t.Fatal(err)
	}
	user := model.User{ID: userID, Username: "kvpage-" + suffix, Role: "user", Active: true}
	if _, err := st.pool.Exec(ctx, `INSERT INTO users(id,username,display_name,role,active)
		VALUES($1,$2,$3,'user',true)`, user.ID, user.Username, "KV Pagination Test"); err != nil {
		t.Fatal(err)
	}
	pagePrefix := "kvpage/" + suffix
	for _, leaf := range []string{"a", "b", "c"} {
		if _, err := st.PutSecret(ctx, model.SecretWrite{
			Path: pagePrefix + "/" + leaf, Data: map[string]any{"value": leaf},
		}, admin.ID); err != nil {
			t.Fatal(err)
		}
	}
	rules, err := json.Marshal(model.PolicyRules{Paths: []model.PathRule{
		{Path: pagePrefix + "/a", Capabilities: []string{"list"}},
		{Path: pagePrefix + "/c", Capabilities: []string{"list"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	policy, err := st.PutPolicy(ctx, "", model.Policy{
		Name: "kv-page-" + suffix, Rules: rules,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetPolicyUsers(ctx, policy.ID, []string{user.ID}); err != nil {
		t.Fatal(err)
	}
	firstPage, err := st.ListSecretsAuthorizedFiltered(ctx, user, pagePrefix, "", 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	secondPage, err := st.ListSecretsAuthorizedFiltered(ctx, user, pagePrefix, "", 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(firstPage) != 1 || firstPage[0].Path != pagePrefix+"/a" {
		t.Fatalf("first authorized page=%#v", firstPage)
	}
	if len(secondPage) != 1 || secondPage[0].Path != pagePrefix+"/c" {
		t.Fatalf("second authorized page=%#v", secondPage)
	}
	personalDashboard, err := st.UserDashboard(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if personalDashboard.Secrets != 2 || personalDashboard.Policies != 1 || personalDashboard.Users != 1 ||
		personalDashboard.RecentAudit == nil {
		t.Fatalf("personal dashboard leaked or omitted scoped inventory: %#v", personalDashboard)
	}

	// `_`는 LIKE 와일드카드이므로 이스케이프하지 않으면 이웃 prefix의 key가 함께 나열됩니다.
	listPrefix := "kvlike_" + suffix
	neighborPrefix := "kvlikeX" + suffix
	for _, full := range []string{listPrefix + "/mine", neighborPrefix + "/theirs"} {
		if _, err := st.PutSecret(ctx, model.SecretWrite{
			Path: full, Data: map[string]any{"value": "x"},
		}, admin.ID); err != nil {
			t.Fatal(err)
		}
	}
	children, err := st.ListSecretChildren(ctx, listPrefix)
	if err != nil {
		t.Fatal(err)
	}
	if len(children) != 1 || children[0] != "mine" {
		t.Fatalf("LIST leaked keys from a neighbouring prefix: %#v", children)
	}
}

func integrationVersion(metadata OpenBaoKVMetadata, version int) (OpenBaoKVVersion, bool) {
	for _, item := range metadata.Versions {
		if item.Version == version {
			return item, true
		}
	}
	return OpenBaoKVVersion{}, false
}
