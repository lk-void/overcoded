// Package nplusone demonstrates the N+1 query problem and its fixes against a
// fake database that charges a fixed network round trip per query.
package nplusone

import (
	"context"
	"sync/atomic"
	"time"
)

type Author struct {
	ID   int
	Name string
}

type Book struct {
	ID       int
	AuthorID int
	Title    string
}

type AuthorBooks struct {
	Author Author
	Books  []Book
}

// AuthorBookRow is one row of Author LEFT JOIN Book. Book.ID is 0 when the
// author has no books.
type AuthorBookRow struct {
	Author Author
	Book   Book
}

// maxInParams mirrors the bind-parameter limits real drivers enforce.
const maxInParams = 500

// DB simulates a database reached over a network. Every method is exactly one
// round trip, and counts as one query.
type DB struct {
	RoundTrip time.Duration // fixed cost per query
	PerRow    time.Duration // marginal cost per returned row

	authors []Author
	books   map[int][]Book
	queries atomic.Int64
}

// NewDB creates authors with booksPerAuthor books each. Every tenth author has
// no books, so join-based fixes have to handle the empty case.
func NewDB(authors, booksPerAuthor int, roundTrip time.Duration) *DB {
	db := &DB{
		RoundTrip: roundTrip,
		PerRow:    time.Microsecond,
		books:     make(map[int][]Book),
	}
	bookID := 1
	for id := 1; id <= authors; id++ {
		db.authors = append(db.authors, Author{ID: id, Name: "author"})
		if id%10 == 0 {
			continue
		}
		for j := 0; j < booksPerAuthor; j++ {
			db.books[id] = append(db.books[id], Book{ID: bookID, AuthorID: id, Title: "book"})
			bookID++
		}
	}
	return db
}

func (db *DB) Queries() int64 { return db.queries.Load() }
func (db *DB) ResetQueries()  { db.queries.Store(0) }

func (db *DB) wait(ctx context.Context, rows int) error {
	if err := ctx.Err(); err != nil {
		return err // a real driver would not send the query
	}
	db.queries.Add(1)
	d := db.RoundTrip + time.Duration(rows)*db.PerRow
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// Authors: SELECT * FROM authors
func (db *DB) Authors(ctx context.Context) ([]Author, error) {
	if err := db.wait(ctx, len(db.authors)); err != nil {
		return nil, err
	}
	return append([]Author(nil), db.authors...), nil
}

// BooksByAuthor: SELECT * FROM books WHERE author_id = $1
func (db *DB) BooksByAuthor(ctx context.Context, authorID int) ([]Book, error) {
	rows := db.books[authorID]
	if err := db.wait(ctx, len(rows)); err != nil {
		return nil, err
	}
	return append([]Book(nil), rows...), nil
}

// BooksByAuthors: SELECT * FROM books WHERE author_id IN ($1, $2, ...)
// The caller must keep len(ids) <= maxInParams.
func (db *DB) BooksByAuthors(ctx context.Context, ids []int) ([]Book, error) {
	var out []Book
	for _, id := range ids {
		out = append(out, db.books[id]...)
	}
	if err := db.wait(ctx, len(out)); err != nil {
		return nil, err
	}
	return out, nil
}

// AuthorBookRows: SELECT ... FROM authors a LEFT JOIN books b ON b.author_id = a.id
func (db *DB) AuthorBookRows(ctx context.Context) ([]AuthorBookRow, error) {
	var out []AuthorBookRow
	for _, a := range db.authors {
		bs := db.books[a.ID]
		if len(bs) == 0 {
			out = append(out, AuthorBookRow{Author: a})
			continue
		}
		for _, b := range bs {
			out = append(out, AuthorBookRow{Author: a, Book: b})
		}
	}
	if err := db.wait(ctx, len(out)); err != nil {
		return nil, err
	}
	return out, nil
}
