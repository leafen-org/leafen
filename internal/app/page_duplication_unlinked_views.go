package app

import (
	"encoding/json"

	"github.com/google/uuid"
	"github.com/pocketbase/pocketbase/core"
)

// A copy into another workspace takes a linked database of a database it
// doesn't take as a linked view not yet linked: its block and its views come
// along, their tabs over "데이터 소스 연결", naming no database — as Notion's
// copy leaves one (seen 2026-11-24 twice, a page with a linked view of a
// database elsewhere in its workspace moved into two other test workspaces,
// and its copy moved back beside that database still waiting). Each source
// it named is named by a stand-in, made from the copy and that source, that
// names no database anywhere, so the linked view reads as one whose database
// is gone for good (database_gone_links_live.go) wherever it is copied or
// moved next, and carries nothing of the workspace it came from. A database
// the copy takes, or one its new workspace already holds, it still shows.
type pageDuplicationUnlinks struct {
	app  core.App
	job  storedPageDuplicationJob
	kept map[string]bool
}

func newPageDuplicationUnlinks(app core.App, job storedPageDuplicationJob) *pageDuplicationUnlinks {
	return &pageDuplicationUnlinks{app: app, job: job, kept: map[string]bool{}}
}

// load sorts the sources a copy's linked databases name into those it keeps
// naming — copied by the job, or in sight in its new workspace — and the rest.
func (unlinks *pageDuplicationUnlinks) load(ids []string) error {
	unknown := []string{}
	for _, id := range ids {
		if _, known := unlinks.kept[id]; !known && id != "" {
			unknown = append(unknown, id)
		}
	}
	if len(unknown) == 0 || !unlinks.job.crossWorkspace() {
		return nil
	}
	copied, err := pageDuplicationReferenceMappings(unlinks.app, unlinks.job.ID, "data_source", unknown)
	if err != nil {
		return err
	}
	inSight, err := dataSourcesInSight(unlinks.app, unlinks.job.WorkspaceID, unknown)
	if err != nil {
		return err
	}
	for _, id := range unknown {
		unlinks.kept[id] = copied[id] != "" || inSight[id]
	}
	return nil
}

// keep has the copy keep naming the sources it takes later, as a database
// page's body copy takes the body's own databases after the rest
// (nameRowPageDatabaseCopy then names their copies).
func (unlinks *pageDuplicationUnlinks) keep(ids map[string]bool) {
	for id := range ids {
		unlinks.kept[id] = true
	}
}

// unlinked says whether the copy names a source by a stand-in.
func (unlinks *pageDuplicationUnlinks) unlinked(id string) (bool, error) {
	if !unlinks.job.crossWorkspace() || id == "" {
		return false, nil
	}
	if err := unlinks.load([]string{id}); err != nil {
		return false, err
	}
	return !unlinks.kept[id], nil
}

// standIn is the ID the copy names a source or a database by in place of one
// it unlinks: the same for the block and its views, and no other's.
func (unlinks *pageDuplicationUnlinks) standIn(id string) string {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte("leafen:copy-unlinked:v1\x00"+unlinks.job.ID+"\x00"+id)).String()
}

// viewSource is the source a copied view of a linked database names.
func (unlinks *pageDuplicationUnlinks) viewSource(id string) (string, error) {
	unlinked, err := unlinks.unlinked(id)
	if err != nil || !unlinked {
		return id, err
	}
	return unlinks.standIn(id), nil
}

// content unlinks, in a database block's content about to be copied, the
// linked members whose sources the copy doesn't keep naming, and says
// whether it changed it.
func (unlinks *pageDuplicationUnlinks) content(content map[string]any) (bool, error) {
	if !unlinks.job.crossWorkspace() {
		return false, nil
	}
	encoded, err := json.Marshal(content)
	if err != nil {
		return false, err
	}
	parsed, err := parseDatabaseContainerBlockContent(encoded)
	if err != nil {
		return false, nil
	}
	linked := parsed.linkedMemberList()
	ids := make([]string, len(linked))
	for index, member := range linked {
		ids[index] = member.DataSourceID
	}
	if err := unlinks.load(ids); err != nil {
		return false, err
	}
	changed := false
	for index, member := range linked {
		if unlinked, err := unlinks.unlinked(member.DataSourceID); err != nil {
			return false, err
		} else if unlinked {
			linked[index] = databaseContainerLinkedMember{
				DataSourceID: unlinks.standIn(member.DataSourceID), SourceContainerID: unlinks.standIn(member.SourceContainerID),
			}
			changed = true
		}
	}
	if !changed {
		return false, nil
	}
	next := withDatabaseContainerMembers(content, parsed.ownDataSourceIDs(), linked)
	clear(content)
	for key, value := range next {
		content[key] = value
	}
	return true, nil
}

// blocks unlinks the copy's database blocks of a window before their
// references are named anew (remapPageDuplicationBlockContents).
func (unlinks *pageDuplicationUnlinks) blocks(blocks []pageDuplicationSourceBlock) error {
	if !unlinks.job.crossWorkspace() {
		return nil
	}
	for index := range blocks {
		if blocks[index].Type != graphBlockTypeInlineDatabase {
			continue
		}
		content := map[string]any{}
		if json.Unmarshal([]byte(blocks[index].Content), &content) != nil {
			continue
		}
		changed, err := unlinks.content(content)
		if err != nil {
			return err
		}
		if !changed {
			continue
		}
		encoded, err := json.Marshal(content)
		if err != nil {
			return err
		}
		blocks[index].Content = string(encoded)
	}
	return nil
}
