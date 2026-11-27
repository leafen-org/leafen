package app

import "github.com/pocketbase/pocketbase/core"

// A reader's closer asked one target at a time read whether the reader may
// open its page for each: a page whose body names many pages — sub-page
// blocks, mentions in its text — read that, some ten queries, once per page
// it names (2026-11-25, NAS condition: a page of 2,100 sub-pages, each named
// in its text, read its graph in 22 windows of some 300 ms, nearly all of it
// that, and its text's mentions took 5–8 s more). A closer told the targets
// it will be asked of works them out together first — one read of the pages
// they name for many — and answers each as it would have alone.

// mentionTarget is what a closer is asked of: a mention's kind (a page or
// database mention, a sub-page block) and the page or database it names.
type mentionTarget struct{ mention, id string }

// mentionLearnBatch bounds the pages a closer works out in one read.
const mentionLearnBatch = 500

// learningCloser is a reader's closer and how to tell it, ahead, targets it
// will be asked of: they are worked out together then, and a target it
// couldn't work out so is asked alone, as before.
type learningCloser struct {
	closes searchMentionCloser
	learn  func(targets []mentionTarget)
}

// learningCloses is the reader's closer (closes) as a learningCloser. Only a
// person reading a page they opened learns ahead so far; another reader's is
// asked a target at a time.
func (reader wordsReader) learningCloses(app core.App) learningCloser {
	if reader.opening && reader.token == nil {
		return ownCopyToComeLearningCloser(app, reader.principalID)
	}
	return learningCloser{closes: reader.closes(app), learn: func([]mentionTarget) {}}
}

// closesKnowing is the reader's closer with the targets worked out ahead.
func (reader wordsReader) closesKnowing(app core.App, targets []mentionTarget) searchMentionCloser {
	closer := reader.learningCloses(app)
	closer.learn(targets)
	return closer.closes
}

// mentionTargetsAsked is what a walk over content asks a closer of, the walk
// told every target opens, so it changes nothing as it goes.
func mentionTargetsAsked(walk func(closer func() searchMentionCloser)) []mentionTarget {
	targets := []mentionTarget{}
	recording := searchMentionCloser(func(mention, targetID string) mentionReach {
		targets = append(targets, mentionTarget{mention: mention, id: targetID})
		return mentionOpens
	})
	walk(func() searchMentionCloser { return recording })
	return targets
}

// readerCloserFor is the closer of a person reading a page they opened
// (readerCloser) for a walk over what they read, told ahead every target the
// walk asks it of. The walk runs twice: first told every target opens, which
// leaves what it reads as it was or writes it again the same, then with the
// closer.
func readerCloserFor(app core.App, readerID string, walk func(closer func() searchMentionCloser)) func() searchMentionCloser {
	targets := mentionTargetsAsked(walk)
	if len(targets) == 0 {
		return readerCloser(app, readerID)
	}
	closes := pageReader(readerID).closesKnowing(app, targets)
	return func() searchMentionCloser { return closes }
}

// ownCopyToComeLearningCloser is ownCopyToComeCloser, which can be told its
// targets ahead.
func ownCopyToComeLearningCloser(app core.App, principalID string) learningCloser {
	known := map[string]mentionReach{}
	closes := func(mention, targetID string) mentionReach {
		key := mention + ":" + targetID
		if reach, seen := known[key]; seen {
			return reach
		}
		pageID := targetID
		if mention == "database" {
			pageID = mcpDatabaseHolderPageID(app, targetID)
		}
		reach := mentionMissing
		if pageID != "" {
			open, err := openablePageIDs(app, principalID, []string{pageID})
			opens, toCome := err == nil && open[pageID], false
			if !opens {
				coming, comeErr := pagesOfOwnCopyToCome(app, principalID, []string{pageID})
				toCome = err == nil && comeErr == nil && coming[pageID]
			}
			reach = ownCopyToComeReach(app, mention, pageID, opens, toCome)
		}
		known[key] = reach
		return reach
	}
	learn := func(targets []mentionTarget) {
		type asked struct{ key, mention, pageID string }
		pending, named := []asked{}, map[string]bool{}
		for _, target := range targets {
			key := target.mention + ":" + target.id
			if _, seen := known[key]; seen || named[key] {
				continue
			}
			named[key] = true
			pageID := target.id
			if target.mention == "database" {
				pageID = mcpDatabaseHolderPageID(app, target.id)
			}
			if pageID == "" {
				known[key] = mentionMissing
				continue
			}
			pending = append(pending, asked{key: key, mention: target.mention, pageID: pageID})
		}
		for start := 0; start < len(pending); start += mentionLearnBatch {
			part := pending[start:min(start+mentionLearnBatch, len(pending))]
			pageIDs, listed := make([]string, 0, len(part)), map[string]bool{}
			for _, one := range part {
				if !listed[one.pageID] {
					listed[one.pageID] = true
					pageIDs = append(pageIDs, one.pageID)
				}
			}
			open, err := openablePageIDs(app, principalID, pageIDs)
			if err != nil {
				// The rest are asked alone, as before.
				return
			}
			closed := []string{}
			for _, pageID := range pageIDs {
				if !open[pageID] {
					closed = append(closed, pageID)
				}
			}
			toCome, err := pagesOfOwnCopyToCome(app, principalID, closed)
			if err != nil {
				return
			}
			for _, one := range part {
				known[one.key] = ownCopyToComeReach(app, one.mention, one.pageID, open[one.pageID], toCome[one.pageID])
			}
		}
	}
	return learningCloser{closes: closes, learn: learn}
}
