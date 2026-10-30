package nplusone

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// BenchmarkLoad shows time and query count as authors grow, with a simulated
// 200µs round trip (a fast same-AZ database).
func BenchmarkLoad(b *testing.B) {
	for _, n := range []int{10, 100, 500} {
		db := NewDB(n, 5, 200*time.Microsecond)
		for _, v := range variants {
			b.Run(fmt.Sprintf("authors=%d/%s", n, v.name), func(b *testing.B) {
				run(b, db, v)
			})
		}
	}
}

// BenchmarkNoLatency removes the network. N+1 looks fine here, which is why it
// survives local development and only hurts in production.
func BenchmarkNoLatency(b *testing.B) {
	db := NewDB(500, 5, 0)
	db.PerRow = 0
	for _, v := range variants {
		b.Run(v.name, func(b *testing.B) { run(b, db, v) })
	}
}

func run(b *testing.B, db *DB, v variant) {
	b.ReportAllocs()
	db.ResetQueries()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := v.fn(context.Background(), db); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportMetric(float64(db.Queries())/float64(b.N), "queries/op")
}
