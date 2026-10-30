package nplusone

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

type variant struct {
	name string
	fn   func(context.Context, *DB) ([]AuthorBooks, error)
}

var variants = []variant{
	{"naive", Naive},
	{"concurrent8", func(ctx context.Context, db *DB) ([]AuthorBooks, error) { return Concurrent(ctx, db, 8) }},
	{"batched", Batched},
	{"joined", Joined},
}

func TestVariantsAgree(t *testing.T) {
	db := NewDB(1200, 3, 0) // 1200 > maxInParams, so Batched must chunk
	want, err := Naive(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range variants[1:] {
		t.Run(v.name, func(t *testing.T) {
			got, err := v.fn(context.Background(), db)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatal("result differs from naive")
			}
		})
	}
}

func TestQueryCounts(t *testing.T) {
	const n = 1200
	tests := []struct {
		v    variant
		want int64
	}{
		{variants[0], 1 + n},
		{variants[1], 1 + n},
		{variants[2], 1 + 3}, // authors + ceil(1200/500) chunks
		{variants[3], 1},
	}
	for _, tt := range tests {
		t.Run(tt.v.name, func(t *testing.T) {
			db := NewDB(n, 3, 0)
			if _, err := tt.v.fn(context.Background(), db); err != nil {
				t.Fatal(err)
			}
			if got := db.Queries(); got != tt.want {
				t.Fatalf("queries = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestCancelStopsEarly(t *testing.T) {
	for _, v := range variants[:2] {
		t.Run(v.name, func(t *testing.T) {
			db := NewDB(1000, 3, time.Millisecond)
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
			defer cancel()
			_, err := v.fn(ctx, db)
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("err = %v, want deadline exceeded", err)
			}
			if q := db.Queries(); q > 200 {
				t.Fatalf("ran %d queries after cancel", q)
			}
		})
	}
}
