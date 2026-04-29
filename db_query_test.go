package main

import (
	"errors"
	"strings"
	"testing"
)

func TestBuildListImageVectorBackfillQuery_Modes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		input ListImageVectorBackfillCandidatesInput
		want  []string // substrings that must appear in WHERE
	}{
		{
			name: "missing",
			input: ListImageVectorBackfillCandidatesInput{
				Mode: "missing", Limit: 10,
			},
			want: []string{`"imageVector" IS NULL`},
		},
		{
			name: "failed",
			input: ListImageVectorBackfillCandidatesInput{
				Mode: "FAILED", Limit: 5,
			},
			want: []string{`"imageVectorStatus" = 'failed'`},
		},
		{
			name: "all",
			input: ListImageVectorBackfillCandidatesInput{
				Mode: "all", Limit: 1,
			},
			want: []string{`btrim("imageFile_id")`},
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			q, args, err := buildListImageVectorBackfillQuery("Photo", tc.input)
			if err != nil {
				t.Fatal(err)
			}
			for _, sub := range tc.want {
				if !strings.Contains(q, sub) {
					t.Fatalf("query missing %q:\n%s", sub, q)
				}
			}
			if len(args) != 1 || args[0] != tc.input.Limit {
				t.Fatalf("args = %v, want single limit %d", args, tc.input.Limit)
			}
		})
	}
}

func TestBuildListImageVectorBackfillQuery_OptionsAndCursor(t *testing.T) {
	t.Parallel()
	input := ListImageVectorBackfillCandidatesInput{
		Mode:              "all",
		Limit:             100,
		Cursor:            "42",
		MaxRetries:        3,
		OnlyOlderThanMins: 30,
	}
	q, args, err := buildListImageVectorBackfillQuery("Photo", input)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(q, `COALESCE("imageVectorRetryCount", 0) < $1`) {
		t.Fatal("expected maxRetries condition")
	}
	if !strings.Contains(q, `' minutes')::interval`) {
		t.Fatal("expected onlyOlderThan condition")
	}
	if !strings.Contains(q, `id > $3`) {
		t.Fatal("expected cursor condition")
	}
	if len(args) != 4 {
		t.Fatalf("args: %v", args)
	}
	if args[0].(int) != 3 || args[1].(int) != 30 || args[2].(int64) != 42 || args[3].(int) != 100 {
		t.Fatalf("unexpected args order/values: %#v", args)
	}
}

func TestBuildListImageVectorBackfillQuery_InvalidMode(t *testing.T) {
	t.Parallel()
	_, _, err := buildListImageVectorBackfillQuery("Photo", ListImageVectorBackfillCandidatesInput{
		Mode: "nope", Limit: 1,
	})
	if err == nil || !strings.Contains(err.Error(), "invalid mode") {
		t.Fatalf("expected invalid mode error, got %v", err)
	}
}

func TestBuildListImageVectorBackfillQuery_InvalidCursor(t *testing.T) {
	t.Parallel()
	_, _, err := buildListImageVectorBackfillQuery("Photo", ListImageVectorBackfillCandidatesInput{
		Mode: "missing", Limit: 1, Cursor: "not-int",
	})
	if err == nil || !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("expected ErrInvalidCursor, got %v", err)
	}
}
