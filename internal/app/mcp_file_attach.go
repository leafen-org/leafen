package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Every MCP path that names a file by its ID goes by one rule here: a page's
// body placing file-upload://<ID> (notion-update-page, notion-create-pages),
// a files property (their properties), a comment's file, a file's new
// version (leafen_replace_file), an attachment made from another
// (notion-create-attachment's source_file_id), a body read from an upload,
// and a text attachment's download. leafen_call's content changes take a
// file by its fileId alone the same way (parseGraphFileBlockContentOf).
//
//   - The ID comes in any form an upload gives it (mcp_file_references.go).
//   - It names a whole file — sent in full, not being deleted — its person
//     may read, in a workspace the connection reaches; however it was
//     uploaded (2026-11-24: an agent's file sent through leafen_call's
//     POST /files was refused by notion-update-page, which took only the
//     connection's own notion-create-file-upload), save one another
//     connection's file tools made and nothing holds yet, which stays that
//     connection's as Notion's uploads stay their integration's. A
//     connection narrowed to chosen pages names by its ID alone only a file
//     its person uploaded, as get-file-download-urls takes no other by ID
//     alone.
//   - Placed, it goes where its target is: in the target's workspace as it
//     is; an upload of its person's placed nowhere yet, in another workspace
//     the connection reaches, is brought there — copied under an ID made
//     from its own and the workspace's, then deleted where it was, so none is
//     left behind and none copied twice. notion-create-file-upload and
//     notion-create-attachment keep Notion's input, with no page, and store
//     into the connection's default workspace (2026-11-24, Tulip: a
//     connection reaching four workspaces uploaded into its default, and the
//     row in another refused it as "file not found" under an ID the agent
//     never gave). A file placed in another workspace stays there.
//   - A refusal names the ID the agent gave, why, and what to do instead.
//
// A copy writes files and can't go inside a write's transaction: a write
// brings its uploads before it starts (mcpBringMarkdownUploads), or, where
// only its transaction knows them, stops with mcpUploadsToBring, brings
// them, and is made again (mcpWithBroughtUploads).

// mcpAttachableFile is the file an agent's reference names, to be placed in
// workspaceID.
func mcpAttachableFile(app core.App, authority mcpAuthority, reference, workspaceID string) (*core.Record, error) {
	id, err := mcpReferencedUploadID(reference)
	if err != nil {
		return nil, err
	}
	if record, brought := mcpBroughtFile(app, authority, id, workspaceID); brought {
		return record, nil
	}
	record, workspace, err := mcpReadableUpload(app, authority, id)
	if err != nil {
		return nil, err
	}
	if workspace.WorkspaceID == workspaceID {
		return record, nil
	}
	placed, err := findActiveProductFileReference(app, id)
	if err != nil {
		return nil, failedBecause(errMCPToolFailed, err)
	}
	if placed || record.GetString("created_by") != authority.PrincipalID {
		return nil, mcpInvalid("file_upload %s is placed in another workspace (%s) and stays there: upload the file again with notion-create-file-upload and attach the new file_upload_id",
			id, workspace.Workspace.GetString("name"))
	}
	if app.IsTransactional() {
		return nil, mcpUploadsToBring{workspaceID: workspaceID, ids: []string{id}}
	}
	return mcpBringUpload(app, authority, record, workspaceID)
}

// mcpReadableUpload is the file an ID names, read by the rule above with no
// target, and its workspace.
func mcpReadableUpload(app core.App, authority mcpAuthority, id string) (*core.Record, workspaceAuthorization, error) {
	record, authorization, err := authorizeWorkspaceFile(app, authority.PrincipalID, id, permissionWorkspaceRead)
	if isPermissionDenied(err) {
		return nil, workspaceAuthorization{}, mcpUploadMissing(app, authority, id)
	}
	if err != nil {
		return nil, workspaceAuthorization{}, failedBecause(errMCPToolFailed, err)
	}
	// Read where it is, in any workspace the connection reaches though the
	// call was held to one (within), and not worked in (mcp_answer_workspaces.go).
	reader := authority
	if authority.whole != nil {
		reader = *authority.whole
	}
	reader.used = nil
	workspace, err := selectMCPWorkspace(app, reader, mcpWorkspaceInput{WorkspaceID: authorization.WorkspaceID}, permissionWorkspaceRead)
	if err != nil {
		if errors.Is(err, errMCPScopeDenied) {
			return nil, workspaceAuthorization{}, mcpDenied("file_upload %s is in a workspace this connection wasn't given: %s", id, mcpWorkspacesListedOn(authority))
		}
		return nil, workspaceAuthorization{}, err
	}
	if authority.PagesNarrowed && record.GetString("created_by") != authority.PrincipalID {
		return nil, workspaceAuthorization{}, mcpDenied("file_upload %s isn't one this account uploaded: a connection narrowed to chosen pages attaches only its own uploads, or a file by the address notion-fetch gave", id)
	}
	if foreign, err := mcpOtherConnectionsUpload(app, authority, id); err != nil {
		return nil, workspaceAuthorization{}, failedBecause(errMCPToolFailed, err)
	} else if foreign {
		return nil, workspaceAuthorization{}, mcpDenied("file_upload %s is another connection's, made with its file tools and placed nowhere yet: upload the file with this connection's notion-create-file-upload", id)
	}
	return record, workspace, nil
}

// mcpOtherConnectionsUpload says an upload another connection's file tools
// made is still that connection's: until it is placed, as Notion's file
// uploads are their integration's. One sent through the API or the app is
// any of its person's connections'.
func mcpOtherConnectionsUpload(app core.App, authority mcpAuthority, id string) (bool, error) {
	upload, err := loadProductFileUploadByID(app, authority.PrincipalID, id)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil || !strings.HasPrefix(upload.RequestID, "mcp-file-request-") ||
		upload.RequestID == mcpFileRequestID(authority.ConnectionID, id) {
		return false, err
	}
	placed, err := findActiveProductFileReference(app, id)
	return !placed, err
}

// mcpReadableUploadOf reads a reference by the rule, with no target.
func mcpReadableUploadOf(app core.App, authority mcpAuthority, reference string) (*core.Record, error) {
	id, err := mcpReferencedUploadID(reference)
	if err != nil {
		return nil, err
	}
	record, _, err := mcpReadableUpload(app, authority, id)
	return record, err
}

func mcpReferencedUploadID(reference string) (string, error) {
	id := mcpUploadedFileID(reference)
	if !fileUploadIDPattern.MatchString(id) {
		return "", mcpInvalid("%q isn't a file_upload_id: give the one notion-create-file-upload or notion-create-attachment answered with, alone or as file-upload://<ID>", strings.TrimSpace(reference))
	}
	return id, nil
}

// mcpUploadMissing says why an ID names no file the connection may attach.
func mcpUploadMissing(app core.App, authority mcpAuthority, id string) error {
	brought := struct {
		WorkspaceID string `db:"workspace_id"`
	}{}
	prefix := mcpBringRequestPrefix(id)
	err := app.DB().NewQuery(`
		SELECT [[workspace_id]] FROM {{leafen_file_uploads}}
		WHERE [[actor_id]] = {:actor} AND [[request_id]] >= {:from} AND [[request_id]] < {:to}
		ORDER BY [[created_at]] LIMIT 1
	`).Bind(dbx.Params{
		"actor": authority.PrincipalID, "from": prefix, "to": strings.TrimSuffix(prefix, ":") + ";",
	}).One(&brought)
	if err == nil {
		name := brought.WorkspaceID
		if workspace, findErr := app.FindRecordById(workspacesCollection, brought.WorkspaceID); findErr == nil {
			name = workspace.GetString("name")
		}
		return mcpInvalid("file_upload %s went into another workspace (%s) when it was attached there: to attach the file here too, upload it again with notion-create-file-upload", id, name)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return failedBecause(errMCPToolFailed, err)
	}
	status := ""
	err = app.DB().NewQuery(`
		SELECT [[status]] FROM {{leafen_file_uploads}}
		WHERE [[actor_id]] = {:actor} AND ([[upload_id]] = {:id} OR [[file_id]] = {:id})
		LIMIT 1
	`).Bind(dbx.Params{"actor": authority.PrincipalID, "id": id}).Row(&status)
	if err == nil && status != fileUploadStatusCompleted {
		return mcpInvalid("file_upload %s isn't sent in full yet: send the file to its upload_url (curl -F file=@<file> <upload_url>), then attach it", id)
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return failedBecause(errMCPToolFailed, err)
	}
	return mcpInvalid("there's no file_upload %s this connection can attach: notion-create-file-upload gives a file_upload_id, attached by it or as file-upload://<ID> once the file is sent to its upload_url", id)
}

// mcpBroughtFile is the copy an upload became in a workspace, once brought.
func mcpBroughtFile(app core.App, authority mcpAuthority, id, workspaceID string) (*core.Record, bool) {
	record, authorization, err := authorizeWorkspaceFile(
		app, authority.PrincipalID, mcpBroughtFileID(authority.PrincipalID, id, workspaceID), permissionWorkspaceRead,
	)
	return record, err == nil && authorization.WorkspaceID == workspaceID
}

// mcpBringUpload copies an upload placed nowhere into a workspace and
// deletes it where it was.
func mcpBringUpload(app core.App, authority mcpAuthority, source *core.Record, workspaceID string) (*core.Record, error) {
	if _, err := selectMCPWorkspace(app, authority, mcpWorkspaceInput{WorkspaceID: workspaceID}, permissionWorkspaceWrite); err != nil {
		return nil, err
	}
	target, _, err := copyFileIntoWorkspace(
		app, personAccessSubject(authority.PrincipalID), authority.PrincipalID,
		source.GetString("workspace"), workspaceID, mcpBringRequestPrefix(source.Id)+workspaceID,
		source.Id, mcpBroughtFileID(authority.PrincipalID, source.Id, workspaceID),
	)
	if err != nil {
		if errors.Is(err, errPageDuplicationConflict) {
			return nil, failedBecause(errMCPToolFailed, err)
		}
		return nil, mcpFileToolError(err)
	}
	// What can't be deleted now the cleanup of uploads never placed takes.
	if _, _, err := deleteProductFileAs(
		context.Background(), app, authority.PrincipalID, source.Id, "mcp-file-brought:"+source.Id,
	); err != nil {
		app.Logger().Warn("an upload brought into another workspace stayed where it was",
			"file", source.Id, "workspace", workspaceID, "error", err)
	}
	return target, nil
}

func mcpBringRequestPrefix(sourceID string) string {
	return "mcp-file-bring:" + sourceID + ":"
}

// mcpBroughtFileID is the ID an upload's copy takes in a workspace.
func mcpBroughtFileID(principalID, sourceID, workspaceID string) string {
	digest := sha256.Sum256([]byte("leafen:mcp-file-brought:v1\x00" + principalID + "\x00" + sourceID + "\x00" + workspaceID))
	return hex.EncodeToString(digest[:])[:15]
}

// mcpUploadsToBring stops a write whose transaction found uploads in another
// workspace, to be brought before it is made again.
type mcpUploadsToBring struct {
	workspaceID string
	ids         []string
}

func (bring mcpUploadsToBring) Error() string {
	return "uploads to move into workspace " + bring.workspaceID + " first"
}

// mcpWithBroughtUploads makes a write, and, each time it stopped for uploads
// to bring, brings them and makes it again.
func mcpWithBroughtUploads(app core.App, authority mcpAuthority, write func() error) error {
	for attempt := 0; ; attempt++ {
		err := write()
		bring := mcpUploadsToBring{}
		if !errors.As(err, &bring) {
			return err
		}
		if attempt == mcpBroughtUploadsMaximum {
			return mcpInvalid("a write attaches at most %d uploads from other workspaces at once: attach the rest in another call", mcpBroughtUploadsMaximum)
		}
		for _, id := range bring.ids {
			if _, err := mcpAttachableFile(app, authority, id, bring.workspaceID); err != nil {
				return err
			}
		}
	}
}

const mcpBroughtUploadsMaximum = 25
