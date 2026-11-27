package app

import (
	"database/sql"
)

// workspaceImportArchivePageOrder gives a staged archive's pages in the order
// they are made: the archive's own, except that a page waits for its parent
// page and comes right after it. An archive lists pages by when they were
// made, so a page moved under one made after it — or pages an earlier import
// made in one go, whose ties fall by id — came before their parent, and its
// import stopped there with "no rows" (2026-11-07: a moved workspace that a
// restore had made). A page whose parent never comes (a loop) keeps its
// place at the end, where the import refuses it as before.
func workspaceImportArchivePageOrder(db *sql.DB) ([]int64, error) {
	rows, err := db.Query(`
		SELECT sequence, COALESCE(json_extract(value_json, '$.id'), ''),
			COALESCE(json_extract(value_json, '$.parentId'), '')
		FROM entities WHERE kind = ? ORDER BY sequence
	`, workspaceExportItemPage)
	if err != nil {
		return nil, workspaceImportStorageError(err)
	}
	defer rows.Close()
	type stagedPage struct {
		sequence   int64
		id, parent string
	}
	pages := []stagedPage{}
	known := map[string]bool{}
	for rows.Next() {
		page := stagedPage{}
		if rows.Scan(&page.sequence, &page.id, &page.parent) != nil {
			return nil, workspaceImportInputError()
		}
		pages = append(pages, page)
		known[page.id] = true
	}
	if err := rows.Err(); err != nil {
		return nil, workspaceImportStorageError(err)
	}
	order := make([]int64, 0, len(pages))
	made := map[string]bool{}
	waiting := map[string][]int{}
	for index, page := range pages {
		if page.parent != "" && page.parent != page.id && known[page.parent] && !made[page.parent] {
			waiting[page.parent] = append(waiting[page.parent], index)
			continue
		}
		stack := []int{index}
		for len(stack) > 0 {
			next := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			order = append(order, pages[next].sequence)
			made[pages[next].id] = true
			children := waiting[pages[next].id]
			delete(waiting, pages[next].id)
			for child := len(children) - 1; child >= 0; child-- {
				stack = append(stack, children[child])
			}
		}
	}
	for index, page := range pages {
		if !made[page.id] {
			order = append(order, pages[index].sequence)
			made[page.id] = true
		}
	}
	return order, nil
}

// ensureWorkspaceImportArchivePageOrder keeps, beside a staged archive, the
// order its pages are made in, once: the transform's page pass reads them by
// it (workspaceImportArchiveTransformQuery).
func ensureWorkspaceImportArchivePageOrder(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS page_order (
		rank INTEGER PRIMARY KEY NOT NULL, sequence INTEGER NOT NULL UNIQUE)`); err != nil {
		return workspaceImportStorageError(err)
	}
	var recorded int
	if err := db.QueryRow(`SELECT COUNT(*) FROM page_order`).Scan(&recorded); err != nil {
		return workspaceImportStorageError(err)
	}
	if recorded != 0 {
		return nil
	}
	order, err := workspaceImportArchivePageOrder(db)
	if err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return workspaceImportStorageError(err)
	}
	defer tx.Rollback()
	for rank, sequence := range order {
		if _, err := tx.Exec(`INSERT INTO page_order (rank, sequence) VALUES (?, ?)`, rank+1, sequence); err != nil {
			return workspaceImportStorageError(err)
		}
	}
	if err := tx.Commit(); err != nil {
		return workspaceImportStorageError(err)
	}
	return nil
}

// workspaceImportArchivePageRankCursor reports whether a page pass's cursor
// is a rank in page_order, as the pass writes it ("000000000012"), rather
// than a page an import begun before this order kept.
func workspaceImportArchivePageRankCursor(cursor string) bool {
	if len(cursor) != 12 {
		return false
	}
	for _, digit := range cursor {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return true
}
