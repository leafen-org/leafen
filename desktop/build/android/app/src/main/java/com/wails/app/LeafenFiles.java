package com.wails.app;

import android.content.ContentResolver;
import android.content.ContentValues;
import android.content.Context;
import android.net.Uri;
import android.os.Build;
import android.os.Environment;
import android.provider.MediaStore;
import android.util.Base64;
import android.webkit.WebView;
import android.widget.Toast;

import androidx.annotation.NonNull;
import androidx.webkit.JavaScriptReplyProxy;
import androidx.webkit.WebMessageCompat;
import androidx.webkit.WebViewCompat;
import androidx.webkit.WebViewFeature;

import org.json.JSONObject;

import java.io.File;
import java.io.FileOutputStream;
import java.io.OutputStream;
import java.util.Collections;
import java.util.HashMap;
import java.util.Map;
import java.util.UUID;

/**
 * Saves a file the window hands over into the device's Downloads, as a page
 * a browser shows would download it. Only the app's own page reaches it, as
 * window.leafenFiles, never a frame inside the page: requests ({id, op:
 * begin|append|finish|abort, name, mime, data}) come by postMessage, the
 * file in base64 pieces, and answers ({id, ok, save, location}) go back.
 */
public class LeafenFiles implements WebViewCompat.WebMessageListener {
    private static final String PAGE_ORIGIN = "https://wails.localhost";
    private static final int MAX_CHUNK_CHARS = 8 * 1024 * 1024;

    private final Context context;
    private final Map<String, Saving> saving = new HashMap<>();

    private static class Saving {
        final OutputStream stream;
        final Uri uri;
        final File file;
        final String name;

        Saving(OutputStream stream, Uri uri, File file, String name) {
            this.stream = stream;
            this.uri = uri;
            this.file = file;
            this.name = name;
        }
    }

    private LeafenFiles(Context context) {
        this.context = context;
    }

    /** Gives the app's page its Downloads store, before the page loads. */
    public static void attach(WebView webView, Context context) {
        if (!WebViewFeature.isFeatureSupported(WebViewFeature.WEB_MESSAGE_LISTENER)) return;
        WebViewCompat.addWebMessageListener(webView, "leafenFiles",
                Collections.singleton(PAGE_ORIGIN), new LeafenFiles(context));
    }

    private static String fileName(String name) {
        String plain = name == null ? "" : name.replaceAll("[\\\\/:*?\"<>|\\p{Cntrl}]", "-").trim();
        if (plain.length() > 200) plain = plain.substring(0, 200);
        return plain.isEmpty() || plain.equals(".") || plain.equals("..") ? "download" : plain;
    }

    @Override
    public void onPostMessage(@NonNull WebView view, @NonNull WebMessageCompat message, @NonNull Uri sourceOrigin,
                              boolean isMainFrame, @NonNull JavaScriptReplyProxy replyProxy) {
        if (!isMainFrame || message.getData() == null) return;
        JSONObject reply = new JSONObject();
        try {
            JSONObject request = new JSONObject(message.getData());
            reply.put("id", request.optString("id"));
            String save = request.optString("save");
            switch (request.optString("op")) {
                case "begin":
                    reply.put("save", begin(fileName(request.optString("name")), request.optString("mime")));
                    reply.put("ok", true);
                    break;
                case "append":
                    String data = request.optString("data");
                    Saving piece = saving.get(save);
                    if (piece == null || data.length() > MAX_CHUNK_CHARS) throw new IllegalStateException("no such save");
                    piece.stream.write(Base64.decode(data, Base64.DEFAULT));
                    reply.put("ok", true);
                    break;
                case "finish":
                    reply.put("location", finish(save));
                    reply.put("ok", true);
                    break;
                case "abort":
                    abort(save);
                    reply.put("ok", true);
                    break;
                default:
                    reply.put("ok", false);
            }
        } catch (Exception error) {
            try {
                reply.put("ok", false);
            } catch (Exception ignored) {
                // The answer stays as it is.
            }
        }
        replyProxy.postMessage(reply.toString());
    }

    private String begin(String name, String mime) throws Exception {
        String id = UUID.randomUUID().toString();
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
            ContentResolver resolver = context.getContentResolver();
            ContentValues values = new ContentValues();
            values.put(MediaStore.Downloads.DISPLAY_NAME, name);
            values.put(MediaStore.Downloads.MIME_TYPE, mime == null || mime.isEmpty() ? "application/octet-stream" : mime);
            values.put(MediaStore.Downloads.IS_PENDING, 1);
            Uri uri = resolver.insert(MediaStore.Downloads.EXTERNAL_CONTENT_URI, values);
            if (uri == null) throw new IllegalStateException("Downloads refused the file");
            saving.put(id, new Saving(resolver.openOutputStream(uri), uri, null, name));
        } else {
            // Before Android 10 the app's own Downloads folder needs no permission.
            File directory = context.getExternalFilesDir(Environment.DIRECTORY_DOWNLOADS);
            if (directory == null) throw new IllegalStateException("no Downloads folder");
            File file = new File(directory, name);
            saving.put(id, new Saving(new FileOutputStream(file), null, file, name));
        }
        return id;
    }

    private String finish(String id) throws Exception {
        Saving done = saving.remove(id);
        if (done == null) throw new IllegalStateException("no such save");
        done.stream.close();
        if (done.uri != null) {
            ContentValues values = new ContentValues();
            values.put(MediaStore.Downloads.IS_PENDING, 0);
            context.getContentResolver().update(done.uri, values, null, null);
        }
        Toast.makeText(context, context.getString(R.string.leafen_saved_to_downloads, done.name), Toast.LENGTH_LONG).show();
        return done.uri != null ? Environment.DIRECTORY_DOWNLOADS + "/" + done.name : done.file.getAbsolutePath();
    }

    private void abort(String id) {
        Saving dropped = saving.remove(id);
        if (dropped == null) return;
        try {
            dropped.stream.close();
        } catch (Exception ignored) {
            // Dropped below either way.
        }
        if (dropped.uri != null) context.getContentResolver().delete(dropped.uri, null, null);
        if (dropped.file != null) dropped.file.delete();
    }
}
