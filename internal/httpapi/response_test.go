package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hkjang/jikim/internal/cryptox"
	"github.com/hkjang/jikim/internal/ids"
	"github.com/hkjang/jikim/internal/model"
	"github.com/hkjang/jikim/internal/store"
	"github.com/jackc/pgx/v5"
)

func TestPageNumbersKeepDefaultsOutsideIntegerRange(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	for _, tc := range []struct {
		name, value   string
		limit, offset int
	}{
		{"empty", "", 50, 0},
		{"zero", "0", 0, 0},
		{"ordinary", "12", 12, 12},
		{"leading zeros", "00012", 12, 12},
		{"maximum", strconv.Itoa(maxInt), maxInt, maxInt},
		{"above maximum", strconv.FormatUint(uint64(maxInt)+1, 10), 50, 0},
		{"positive wrap", "18446744073709551617", 50, 0},
		{"long number", strings.Repeat("9", 100), 50, 0},
		{"negative", "-1", 50, 0},
		{"plus sign", "+1", 50, 0},
		{"whitespace", " 1", 50, 0},
		{"trailing junk", "12x", 50, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			query := url.Values{"limit": {tc.value}, "offset": {tc.value}}
			request := httptest.NewRequest(http.MethodGet, "/?"+query.Encode(), nil)
			limit, offset := parsePage(request)
			if limit != tc.limit || offset != tc.offset {
				t.Fatalf("limit=%d offset=%d, want limit=%d offset=%d", limit, offset, tc.limit, tc.offset)
			}
		})
	}
}

// Exercise New's real authentication, routing and PostgreSQL query, without
// replacing a Server dependency. An isolated schema keeps fixture data apart
// from the other integration tests using this disposable database.
func TestApplicationPaginationOverflowIntegration(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("JIKIM_TEST_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip("JIKIM_TEST_POSTGRES_DSN is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(context.Background())
	suffix, err := ids.UUID()
	if err != nil {
		t.Fatal(err)
	}
	schema := "pagination_" + strings.ReplaceAll(suffix, "-", "")
	quotedSchema := pgx.Identifier{schema}.Sanitize()
	if _, err := conn.Exec(ctx, "CREATE SCHEMA "+quotedSchema); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if _, err := conn.Exec(cleanupCtx, "DROP SCHEMA "+quotedSchema+" CASCADE"); err != nil {
			t.Errorf("clean up pagination schema: %v", err)
		}
	}()
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		parsed, err := url.Parse(dsn)
		if err != nil {
			t.Fatal(err)
		}
		query := parsed.Query()
		query.Set("search_path", schema)
		parsed.RawQuery = query.Encode()
		dsn = parsed.String()
	} else {
		dsn += " search_path=" + schema
	}
	master, err := cryptox.New(bytes.Repeat([]byte{0x4b}, cryptox.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.New(ctx, dsn, master)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	admin, _, err := st.BootstrapAdmin(ctx, "pagination-admin", "pagination-test-only-password")
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := st.CreateSession(ctx, admin.ID, "session", "pagination test", admin.ID, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	wantIDs := make([]string, 0, 3)
	for _, name := range []string{"pagination-a", "pagination-b", "pagination-c"} {
		item, err := st.PutApplication(ctx, "", model.Application{Name: name})
		if err != nil {
			t.Fatal(err)
		}
		wantIDs = append(wantIDs, item.ID)
	}
	handler := New(st, slog.New(slog.NewTextHandler(io.Discard, nil)))
	for _, tc := range []struct {
		name, query string
		want        []string
	}{
		{"default page", "", wantIDs},
		{"ordinary page", "limit=1&offset=1", wantIDs[1:2]},
		{"overflowing limit", "limit=18446744073709551617", wantIDs},
		{"overflowing offset", "offset=18446744073709551617", wantIDs},
		{"independent limit fallback", "limit=18446744073709551617&offset=1", wantIDs[1:]},
		{"independent offset fallback", "limit=1&offset=18446744073709551617", wantIDs[:1]},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/api/v1/applications?"+tc.query, nil)
			request.Header.Set("Authorization", "Bearer "+token)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusOK {
				t.Fatalf("status=%d, want 200", response.Code)
			}
			var result struct {
				Data []model.Application `json:"data"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			gotIDs := make([]string, 0, len(result.Data))
			for _, item := range result.Data {
				gotIDs = append(gotIDs, item.ID)
			}
			if !slices.Equal(gotIDs, tc.want) {
				t.Fatalf("returned %d applications, want %d in the expected order", len(gotIDs), len(tc.want))
			}
		})
	}
}
