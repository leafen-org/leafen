package app

import (
	"sync"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A statement a transaction runs again and again is prepared once in it
// (2026-10-14: SQLite compiled a database row's write, its triggers
// included, for every row a CSV import wrote — most of the import's time).
// The statements a transaction prepared close when it ends.

var preparedStatements = struct {
	sync.Mutex
	byTransaction map[*core.TxAppInfo]map[string]*dbx.Query
}{byTransaction: map[*core.TxAppInfo]map[string]*dbx.Query{}}

// preparedQuery is the statement prepared in the transaction app runs in,
// bound afresh by its caller; outside a transaction, a query of its own.
func preparedQuery(app core.App, statement string) *dbx.Query {
	transaction := app.TxInfo()
	if transaction == nil {
		return app.DB().NewQuery(statement)
	}
	preparedStatements.Lock()
	queries, known := preparedStatements.byTransaction[transaction]
	if !known {
		queries = map[string]*dbx.Query{}
		preparedStatements.byTransaction[transaction] = queries
	}
	query := queries[statement]
	preparedStatements.Unlock()
	if !known {
		transaction.OnComplete(func(error) error {
			preparedStatements.Lock()
			prepared := preparedStatements.byTransaction[transaction]
			delete(preparedStatements.byTransaction, transaction)
			preparedStatements.Unlock()
			for _, query := range prepared {
				_ = query.Close()
			}
			return nil
		})
	}
	if query != nil {
		return query
	}
	query = app.DB().NewQuery(statement).Prepare()
	if query.LastError != nil {
		return app.DB().NewQuery(statement)
	}
	preparedStatements.Lock()
	queries[statement] = query
	preparedStatements.Unlock()
	return query
}
