package app

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

// A run asks Notion for a turn's pages' and blocks' comments as many at once
// as the connection's pace takes (notionProviderMaximumConcurrentRequests),
// and keeps what came in the turn's order. One at a time, each ask waited
// for Notion's answer before the next went, and a 31,000-block move was
// reckoned at 376 minutes (2026-11-16: 0.73 s a block against the pace's
// 0.33 s); three at once asked 2.67 blocks a second where one at a time
// asked 1.33, on the same move the same evening. A reading of the person's
// Notion narrows the asks to the blocks with an open discussion
// (notion_migration_comment_discussions.go). When Notion asks the connection
// to wait partway through a turn, the turn keeps the answers before that ask
// and the next turn asks on from there once the wait is over: a turn that
// handed the wait back with nothing kept asked its blocks again from the
// first, and with Notion asking to wait every forty asks a turn of fifty
// never came through (2026-11-21: a run's comments stood at 500 of 19,001
// blocks for minutes while Notion was asked on).

// notionMigrationCommentAnswer is what Notion gave for one page or block, as
// readNotionMigrationComments reads it.
type notionMigrationCommentAnswer struct {
	comments []notionProviderComment
	readable bool
	err      error
}

// askNotionMigrationComments asks for each id's comments and gives the
// answers in the ids' order. The caller goes through them in that order and
// stops at the first that failed, as it did asking one at a time; once one
// fails no more are asked (those after it are answered by the failure), and
// what was asked from it on is asked again by the next turn.
// asking hears each ask as it goes, for Leafen's import window to name it
// (notion_migration_live.go).
func askNotionMigrationComments(
	ctx context.Context, client notionProviderClient, ids []string, asking func(index int),
) []notionMigrationCommentAnswer {
	answers := make([]notionMigrationCommentAnswer, len(ids))
	var group sync.WaitGroup
	var failed atomic.Pointer[error]
	slots := make(chan struct{}, notionProviderMaximumConcurrentRequests)
	for index, id := range ids {
		slots <- struct{}{}
		if err := failed.Load(); err != nil {
			<-slots
			answers[index].err = *err
			continue
		}
		group.Go(func() {
			defer func() { <-slots }()
			answer := &answers[index]
			asking(index)
			answer.comments, answer.readable, answer.err = readNotionMigrationComments(ctx, client, id)
			if answer.err != nil {
				failed.CompareAndSwap(nil, &answer.err)
			}
		})
	}
	group.Wait()
	return answers
}

// notionMigrationCommentsTurnEnd is, for the answer at index that failed with
// err, whether the turn ends before it and keeps the answers before it, or
// the failure the turn ends with. It ends when Notion asked the connection to
// wait (the next turn asks on once the wait is over) or when the server is
// stopping — ctx is the runner's, and the asks under way end with it — so the
// next start asks on from there. Stopped at its first answer, the turn keeps
// nothing and hands the stop back, and the job stays as it was. A stop that
// waited for the turn's fifty asks, three at once, outlasted the server's few
// seconds to stop (2026-11-21: every stop during the comments logged
// LEAFEN_JOB_SHUTDOWN_FAILED).
func notionMigrationCommentsTurnEnd(ctx context.Context, payload *notionImportJobPayload, index int, err error) (bool, error) {
	if err == nil {
		return false, nil
	}
	if stopping := ctx.Err(); stopping != nil {
		if index == 0 {
			return false, stopping
		}
		return true, nil
	}
	if until, wait := notionMigrationCommentsWaitAfter(err); wait && index > 0 {
		payload.commentsAskAfter = until
		return true, nil
	}
	return false, err
}

// notionMigrationCommentsWaitAfter is when Notion asked the connection to
// wait until, for an ask it answered so; a turn stopped there keeps what it
// asked before (notionImportJobPayload.commentsAskAfter).
func notionMigrationCommentsWaitAfter(err error) (time.Time, bool) {
	var limited *notionProviderRateLimitError
	if errors.As(err, &limited) {
		return limited.retryAt, true
	}
	return time.Time{}, false
}
