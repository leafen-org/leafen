package com.wails.app;

import android.net.Uri;
import android.util.Log;
import android.webkit.WebResourceResponse;

import androidx.annotation.NonNull;
import androidx.annotation.Nullable;
import androidx.webkit.WebViewAssetLoader;

import java.io.ByteArrayInputStream;
import java.io.InputStream;
import java.util.HashMap;
import java.util.Map;

/**
 * WailsPathHandler implements WebViewAssetLoader.PathHandler to serve assets
 * from the Go asset server. This allows the WebView to load assets without
 * using a network server, similar to iOS's WKURLSchemeHandler.
 */
public class WailsPathHandler implements WebViewAssetLoader.PathHandler {
    private static final String TAG = "WailsPathHandler";
    private static final boolean DEBUG = BuildConfig.DEBUG;

    private final WailsBridge bridge;

    public WailsPathHandler(WailsBridge bridge) {
        this.bridge = bridge;
    }

    @Nullable
    @Override
    public WebResourceResponse handle(@NonNull String path) {
        if (DEBUG) Log.d(TAG, "Handling path: " + path);

        // Normalize path
        if (path.isEmpty() || path.equals("/")) {
            path = "/index.html";
        }

        // Get asset from Go
        byte[] data = bridge.serveAsset(path, "GET", "{}");

        if (data == null || data.length == 0) {
            Log.w(TAG, "Asset not found: " + path);
            return null; // Return null to let WebView handle 404
        }

        // Determine MIME type
        String mimeType = bridge.getAssetMimeType(path);
        // Leafen: a route of the page (/p/…, /apps/…) has no extension and
        // comes back as the page itself; as a download the window would
        // never open it.
        if ((mimeType == null || mimeType.equals("application/octet-stream")) && isHtmlDocument(data)) {
            mimeType = "text/html";
        }
        if (DEBUG) Log.d(TAG, "Serving " + path + " with type " + mimeType + " (" + data.length + " bytes)");

        // Create response
        InputStream inputStream = new ByteArrayInputStream(data);
        Map<String, String> headers = new HashMap<>();
        headers.put("Access-Control-Allow-Origin", "*");
        headers.put("Cache-Control", "no-cache");

        return new WebResourceResponse(
                mimeType,
                "UTF-8",
                200,
                "OK",
                headers,
                inputStream
        );
    }

    /** Whether the bytes begin an HTML document. */
    private static boolean isHtmlDocument(byte[] data) {
        int start = 0;
        while (start < data.length && start < 64 && Character.isWhitespace(data[start])) start++;
        String head = new String(data, start, Math.min(data.length - start, 15), java.nio.charset.StandardCharsets.US_ASCII)
                .toLowerCase(java.util.Locale.ROOT);
        return head.startsWith("<!doctype html") || head.startsWith("<html");
    }
}
