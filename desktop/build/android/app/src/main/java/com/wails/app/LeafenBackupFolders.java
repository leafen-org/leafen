package com.wails.app;

import android.app.Activity;
import android.content.ContentResolver;
import android.content.Intent;
import android.database.Cursor;
import android.net.Uri;
import android.os.ParcelFileDescriptor;
import android.provider.DocumentsContract;
import android.provider.DocumentsContract.Document;

import org.json.JSONArray;
import org.json.JSONException;
import org.json.JSONObject;

import java.io.InputStream;
import java.io.OutputStream;

/**
 * A backup folder for the app's own server, picked with the system's
 * document-tree picker and kept with a persisted permission: a phone's
 * folders are reached through its document providers, not as paths. The
 * server (Go) asks here for what it does there, by a path below the picked
 * folder: look, list, make folders, open files to write or read (as file
 * descriptors it then owns), move and remove.
 */
public final class LeafenBackupFolders {
    static final int REQUEST_CODE = 0x4842;

    // Answers for a file that can't be opened.
    private static final int MISSING = -1;
    private static final int EXISTS = -2;
    private static final int FAILED = -3;

    private static Activity activity;
    private static int pendingCallback = -1;

    private LeafenBackupFolders() {
    }

    private static native void nativeAttach();

    private static native void nativeChosen(int callbackID, String tree, String name);

    /** Lets the server reach here, once the activity is up. */
    static void attach(Activity owner) {
        activity = owner;
        nativeAttach();
    }

    /** Shows the picker for one folder; what was picked goes back to the server. */
    public static void choose(final int callbackID) {
        final Activity owner = activity;
        if (owner == null) {
            nativeChosen(callbackID, "", "");
            return;
        }
        owner.runOnUiThread(() -> {
            if (pendingCallback != -1) nativeChosen(pendingCallback, "", "");
            pendingCallback = callbackID;
            Intent intent = new Intent(Intent.ACTION_OPEN_DOCUMENT_TREE);
            intent.addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION | Intent.FLAG_GRANT_WRITE_URI_PERMISSION
                    | Intent.FLAG_GRANT_PERSISTABLE_URI_PERMISSION | Intent.FLAG_GRANT_PREFIX_URI_PERMISSION);
            try {
                owner.startActivityForResult(intent, REQUEST_CODE);
            } catch (RuntimeException error) {
                int waiting = pendingCallback;
                pendingCallback = -1;
                nativeChosen(waiting, "", "");
            }
        });
    }

    /** The picker's answer, from the activity; false when it is someone else's. */
    static boolean onActivityResult(int requestCode, int resultCode, Intent data) {
        if (requestCode != REQUEST_CODE) return false;
        int waiting = pendingCallback;
        pendingCallback = -1;
        if (waiting == -1) return true;
        Uri tree = resultCode == Activity.RESULT_OK && data != null ? data.getData() : null;
        if (tree == null) {
            nativeChosen(waiting, "", "");
            return true;
        }
        try {
            resolver().takePersistableUriPermission(tree,
                    Intent.FLAG_GRANT_READ_URI_PERMISSION | Intent.FLAG_GRANT_WRITE_URI_PERMISSION);
            String root = DocumentsContract.getTreeDocumentId(tree);
            JSONObject shown = describe(tree, root);
            nativeChosen(waiting, tree.toString(), shown != null ? shown.optString("name") : "");
        } catch (RuntimeException error) {
            nativeChosen(waiting, "", "");
        }
        return true;
    }

    private static ContentResolver resolver() {
        return activity.getApplicationContext().getContentResolver();
    }

    private static Uri document(Uri tree, String id) {
        return DocumentsContract.buildDocumentUriUsingTree(tree, id);
    }

    private static String parentOf(String path) {
        int slash = path.lastIndexOf('/');
        return slash < 0 ? "" : path.substring(0, slash);
    }

    private static String nameOf(String path) {
        return path.substring(path.lastIndexOf('/') + 1);
    }

    /** The document at a path below the picked folder, or null. */
    private static String find(Uri tree, String path) {
        String id = DocumentsContract.getTreeDocumentId(tree);
        for (String part : path.split("/")) {
            if (part.isEmpty()) continue;
            id = child(tree, id, part);
            if (id == null) return null;
        }
        return id;
    }

    private static String child(Uri tree, String parent, String name) {
        Uri children = DocumentsContract.buildChildDocumentsUriUsingTree(tree, parent);
        try (Cursor cursor = resolver().query(children,
                new String[] {Document.COLUMN_DOCUMENT_ID, Document.COLUMN_DISPLAY_NAME}, null, null, null)) {
            if (cursor == null) return null;
            while (cursor.moveToNext()) {
                if (name.equals(cursor.getString(1))) return cursor.getString(0);
            }
        }
        return null;
    }

    private static JSONObject entry(Cursor cursor) {
        try {
            JSONObject entry = new JSONObject();
            entry.put("name", cursor.getString(1));
            entry.put("dir", Document.MIME_TYPE_DIR.equals(cursor.getString(2)));
            entry.put("size", cursor.isNull(3) ? 0 : cursor.getLong(3));
            entry.put("modified", cursor.isNull(4) ? 0 : cursor.getLong(4));
            return entry;
        } catch (JSONException error) {
            return null;
        }
    }

    private static final String[] COLUMNS = new String[] {
        Document.COLUMN_DOCUMENT_ID, Document.COLUMN_DISPLAY_NAME, Document.COLUMN_MIME_TYPE,
        Document.COLUMN_SIZE, Document.COLUMN_LAST_MODIFIED,
    };

    private static JSONObject describe(Uri tree, String id) {
        try (Cursor cursor = resolver().query(document(tree, id), COLUMNS, null, null, null)) {
            if (cursor == null || !cursor.moveToFirst()) return null;
            return entry(cursor);
        }
    }

    /** A file or folder below the picked one, as JSON, or null when it isn't there. */
    public static String stat(String treeText, String path) {
        try {
            Uri tree = Uri.parse(treeText);
            String id = find(tree, path);
            if (id == null) return null;
            JSONObject entry = describe(tree, id);
            return entry == null ? null : entry.toString();
        } catch (RuntimeException error) {
            return null;
        }
    }

    /** A folder's entries, as a JSON array, or null when it isn't there. */
    public static String list(String treeText, String path) {
        try {
            Uri tree = Uri.parse(treeText);
            String id = find(tree, path);
            if (id == null) return null;
            JSONArray entries = new JSONArray();
            Uri children = DocumentsContract.buildChildDocumentsUriUsingTree(tree, id);
            try (Cursor cursor = resolver().query(children, COLUMNS, null, null, null)) {
                if (cursor == null) return null;
                while (cursor.moveToNext()) {
                    JSONObject entry = entry(cursor);
                    if (entry != null) entries.put(entry);
                }
            }
            return entries.toString();
        } catch (RuntimeException error) {
            return null;
        }
    }

    /** Makes a folder and the folders above it. */
    public static boolean makeDir(String treeText, String path) {
        try {
            Uri tree = Uri.parse(treeText);
            String id = DocumentsContract.getTreeDocumentId(tree);
            for (String part : path.split("/")) {
                if (part.isEmpty()) continue;
                String next = child(tree, id, part);
                if (next == null) {
                    Uri made = DocumentsContract.createDocument(resolver(), document(tree, id), Document.MIME_TYPE_DIR, part);
                    if (made == null) return false;
                    next = DocumentsContract.getDocumentId(made);
                }
                id = next;
            }
            return true;
        } catch (Exception error) {
            return false;
        }
    }

    /**
     * Opens a file to write, as a descriptor the caller then owns: a new one
     * (EXISTS when one is there) or one written whole over the one there.
     */
    public static int openWrite(String treeText, String path, boolean create) {
        try {
            Uri tree = Uri.parse(treeText);
            String parent = find(tree, parentOf(path));
            if (parent == null) return MISSING;
            String existing = child(tree, parent, nameOf(path));
            if (existing != null && create) return EXISTS;
            Uri target = existing != null ? document(tree, existing)
                    : DocumentsContract.createDocument(resolver(), document(tree, parent), "application/octet-stream", nameOf(path));
            if (target == null) return FAILED;
            ParcelFileDescriptor descriptor = resolver().openFileDescriptor(target, "wt");
            return descriptor == null ? FAILED : descriptor.detachFd();
        } catch (Exception error) {
            return FAILED;
        }
    }

    /** Opens a file to read, as a descriptor the caller then owns. */
    public static int openRead(String treeText, String path) {
        try {
            Uri tree = Uri.parse(treeText);
            String id = find(tree, path);
            if (id == null) return MISSING;
            ParcelFileDescriptor descriptor = resolver().openFileDescriptor(document(tree, id), "r");
            return descriptor == null ? FAILED : descriptor.detachFd();
        } catch (Exception error) {
            return FAILED;
        }
    }

    /**
     * Moves a file or folder to another path below the picked folder; false
     * when it isn't there, the target is, or the provider refuses. A move to
     * another folder is the provider's own where it offers one, else a copy
     * of the file and a removal.
     */
    public static boolean rename(String treeText, String from, String to) {
        try {
            Uri tree = Uri.parse(treeText);
            String id = find(tree, from);
            if (id == null || find(tree, to) != null) return false;
            String toParent = find(tree, parentOf(to));
            if (toParent == null) return false;
            Uri moved = document(tree, id);
            if (!parentOf(from).equals(parentOf(to))) {
                String fromParent = find(tree, parentOf(from));
                Uri relocated = null;
                try {
                    relocated = DocumentsContract.moveDocument(resolver(), moved, document(tree, fromParent), document(tree, toParent));
                } catch (Exception unsupported) {
                    relocated = null;
                }
                if (relocated == null) return copyThenRemove(tree, id, toParent, nameOf(to));
                moved = relocated;
            }
            if (!nameOf(from).equals(nameOf(to))) {
                moved = DocumentsContract.renameDocument(resolver(), moved, nameOf(to));
            }
            return moved != null;
        } catch (Exception error) {
            return false;
        }
    }

    private static boolean copyThenRemove(Uri tree, String id, String toParent, String name) throws Exception {
        Uri source = document(tree, id);
        Uri target = DocumentsContract.createDocument(resolver(), document(tree, toParent), "application/octet-stream", name);
        if (target == null) return false;
        try (InputStream input = resolver().openInputStream(source);
             OutputStream output = resolver().openOutputStream(target, "wt")) {
            if (input == null || output == null) return false;
            byte[] buffer = new byte[64 * 1024];
            for (int read = input.read(buffer); read >= 0; read = input.read(buffer)) output.write(buffer, 0, read);
        }
        return DocumentsContract.deleteDocument(resolver(), source);
    }

    /** Removes a file, or a folder with what it holds; true when it is gone. */
    public static boolean removeAll(String treeText, String path) {
        try {
            Uri tree = Uri.parse(treeText);
            String id = find(tree, path);
            if (id == null) return true;
            return DocumentsContract.deleteDocument(resolver(), document(tree, id));
        } catch (Exception error) {
            return false;
        }
    }
}
