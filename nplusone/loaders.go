package nplusone

import (
	"context"
	"slices"
	"sync"
)

// Naive is the N+1: one query for the authors, then one per author.
func Naive(ctx context.Context, db *DB) ([]AuthorBooks, error) {
	authors, err := db.Authors(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]AuthorBooks, 0, len(authors))
	for _, a := range authors {
		books, err := db.BooksByAuthor(ctx, a.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, AuthorBooks{Author: a, Books: nilIfEmpty(books)})
	}
	return out, nil
}

// Concurrent is the tempting band-aid: still N+1 queries, just overlapped.
// Latency drops, but database load, connection use and pool pressure do not.
func Concurrent(ctx context.Context, db *DB, workers int) ([]AuthorBooks, error) {
	authors, err := db.Authors(ctx)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	out := make([]AuthorBooks, len(authors))
	sem := make(chan struct{}, workers)
	var (
		wg       sync.WaitGroup
		once     sync.Once
		firstErr error
	)
	for i, a := range authors {
		if ctx.Err() != nil {
			break
		}
		sem <- struct{}{}
		wg.Add(1)
		go func() {
			defer func() { <-sem; wg.Done() }()
			books, err := db.BooksByAuthor(ctx, a.ID)
			if err != nil {
				once.Do(func() { firstErr = err; cancel() })
				return
			}
			out[i] = AuthorBooks{Author: a, Books: nilIfEmpty(books)}
		}()
	}
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// Batched is the fix most codebases want: 1 query for authors, then
// ceil(N/maxInParams) queries using IN, grouped in memory.
func Batched(ctx context.Context, db *DB) ([]AuthorBooks, error) {
	authors, err := db.Authors(ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]int, len(authors))
	for i, a := range authors {
		ids[i] = a.ID
	}

	byAuthor := make(map[int][]Book, len(authors))
	for chunk := range slices.Chunk(ids, maxInParams) {
		books, err := db.BooksByAuthors(ctx, chunk)
		if err != nil {
			return nil, err
		}
		for _, b := range books {
			byAuthor[b.AuthorID] = append(byAuthor[b.AuthorID], b)
		}
	}

	out := make([]AuthorBooks, len(authors))
	for i, a := range authors {
		out[i] = AuthorBooks{Author: a, Books: byAuthor[a.ID]}
	}
	return out, nil
}

// Joined is a single query. Cheapest in round trips, but every author column is
// repeated on every book row, so wide parents get expensive to transfer.
func Joined(ctx context.Context, db *DB) ([]AuthorBooks, error) {
	rows, err := db.AuthorBookRows(ctx)
	if err != nil {
		return nil, err
	}
	var out []AuthorBooks
	for _, r := range rows {
		if n := len(out); n == 0 || out[n-1].Author.ID != r.Author.ID {
			out = append(out, AuthorBooks{Author: r.Author})
		}
		if r.Book.ID != 0 {
			last := &out[len(out)-1]
			last.Books = append(last.Books, r.Book)
		}
	}
	return out, nil
}

func nilIfEmpty(b []Book) []Book {
	if len(b) == 0 {
		return nil
	}
	return b
}
