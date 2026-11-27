package app

import (
	"cmp"
	"encoding/json"
	"sort"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// Every write of a block checks the files it names by one rule, the graph's
// (validateAndCanonicalizeGraphFileReferences): each file a file, picture,
// video, sound, PDF or embed block, a callout's icon or a files value names is
// one uploaded whole into the page's workspace, of the block's kind, and the
// block takes the file's name, size and type. A content-graph change runs it
// in its validation (validateGraphMutation). A first body given from outside
// is written as a page's first body (writeFirstPageBody), not as a change —
// notion-create-pages' Markdown on an ordinary page, a database page's content
// through the public API, a template's body given as blocks — and runs it here
// before it is written. Markdown placing only finds the file a line names
// (notionMarkdownFiles.place) and leaves the rule to this one check. Before
// (2026-11-26) such a body checked its files where it was made or not at all:
// placing held the graph's rule twice over, and a template's body given as
// blocks kept a text file in a picture, another workspace's picture, and a text
// file as a callout's icon. Copies, imports and built-in bodies write blocks
// already stored, checked when they were written, and don't come here.

// checkGivenPageBodyFiles passes a given first body's blocks through the
// graph's file check, their content then the files'.
func checkGivenPageBodyFiles(app core.App, workspaceID string, operations []graphOperation) error {
	return validateAndCanonicalizeGraphFileReferences(app, workspaceID, operations)
}

// checkGivenPageBodyBlockFiles is checkGivenPageBodyFiles for a body given as
// a page's body blocks.
func checkGivenPageBodyBlockFiles(app core.App, workspaceID string, blocks []newRowPageBlock) error {
	operations := make([]graphOperation, len(blocks))
	for index, block := range blocks {
		content, err := json.Marshal(block.Content)
		if err != nil {
			return err
		}
		operations[index] = graphOperation{Kind: graphOperationKindUpsertBlock, Block: &graphBlock{
			ID: block.ID, Type: block.Type, Content: content,
		}}
	}
	if err := checkGivenPageBodyFiles(app, workspaceID, operations); err != nil {
		return err
	}
	for index := range blocks {
		content := map[string]any{}
		if err := json.Unmarshal(operations[index].Block.Content, &content); err != nil {
			return err
		}
		blocks[index].Content = content
	}
	return nil
}

// graphFilesWhole are the files fileIDs name that are whole in the
// workspace, refusing the first that isn't, by its ID, with the way to one
// that is.
func graphFilesWhole(app core.App, workspaceID string, fileIDs []string) (map[string]graphFileBlockContent, error) {
	records, err := availableGraphFileRecords(app, workspaceID, fileIDs)
	if err != nil {
		return nil, err
	}
	missing := []string{}
	for _, fileID := range fileIDs {
		if _, whole := records[fileID]; !whole {
			missing = append(missing, fileID)
		}
	}
	if len(missing) == 0 {
		return records, nil
	}
	sort.Strings(missing)
	return nil, graphInputError(errorCodeGraphReferenceInvalid, "Check the file link: file "+missing[0]+
		" isn't one uploaded whole into this page's workspace. Finish its upload, or upload the file into this workspace and name the new one.")
}

// graphFileKindRefused refuses a file of another kind than its block holds,
// saying which kind the block takes and what the file is.
func graphFileKindRefused(blockType string, file graphFileBlockContent) error {
	kind := strings.TrimSpace(strings.SplitN(file.ContentType, ";", 2)[0])
	if kind == "" {
		kind = "of no type"
	}
	holds := map[string]string{
		graphBlockTypeImage: "an image block holds a picture (image/…)",
		graphBlockTypeVideo: "a video block holds a video (video/… or application/mp4)",
		graphBlockTypeAudio: "an audio block holds a sound (audio/…)",
		graphBlockTypePDF:   "a PDF block holds a PDF (application/pdf)",
	}[blockType]
	if holds == "" {
		holds = "a " + blockType + " block holds a file Leafen keeps, not an active one"
	}
	return graphInputError(errorCodeGraphReferenceInvalid, "Check the file block type and file kind: "+holds+
		", and "+graphFileNamed(file)+" is "+kind+". Place it as a file block, or name a file of that kind.")
}

// graphCalloutIconKindRefused refuses a callout's icon file that is no picture.
func graphCalloutIconKindRefused(file graphFileBlockContent) error {
	return graphInputError(errorCodeGraphReferenceInvalid, "A callout's icon file must be a picture, and "+
		graphFileNamed(file)+" isn't one: give an uploaded picture, an emoji or an image address.")
}

// graphFileNamed names a file in a refusal by its name, or as the file.
func graphFileNamed(file graphFileBlockContent) string {
	return cmp.Or(strings.TrimSpace(file.Name), "the file")
}
