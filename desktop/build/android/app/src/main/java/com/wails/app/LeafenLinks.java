package com.wails.app;

import android.net.Uri;
import android.webkit.WebView;

import androidx.annotation.NonNull;
import androidx.webkit.JavaScriptReplyProxy;
import androidx.webkit.WebMessageCompat;
import androidx.webkit.WebViewCompat;
import androidx.webkit.WebViewFeature;

import org.json.JSONArray;
import org.json.JSONObject;

import java.util.ArrayList;
import java.util.Collections;
import java.util.List;

/**
 * Hands the app's page the leafen: links Android opened the app with
 * (docs/app-platform.md 17). Each waits here until the page takes it — the
 * page may still be loading, or starting over on another server — and the
 * page that last asked hears when one comes. Only the app's own page reaches
 * it, as window.leafenLinks: it sends {op: "take"} and gets {links}; a
 * {waiting: true} says to take again.
 */
public class LeafenLinks implements WebViewCompat.WebMessageListener {
    private static final String PAGE_ORIGIN = "https://wails.localhost";
    private static final String SCHEME = "leafen";
    private static final int MAX_LINK = 4096;
    private static final int MAX_WAITING = 8;

    private final List<String> waiting = new ArrayList<>();
    private JavaScriptReplyProxy page;

    /** Gives the app's page its links, before the page loads. */
    public static LeafenLinks attach(WebView webView) {
        LeafenLinks links = new LeafenLinks();
        if (WebViewFeature.isFeatureSupported(WebViewFeature.WEB_MESSAGE_LISTENER)) {
            WebViewCompat.addWebMessageListener(webView, "leafenLinks",
                    Collections.singleton(PAGE_ORIGIN), links);
        }
        return links;
    }

    @Override
    public void onPostMessage(@NonNull WebView view, @NonNull WebMessageCompat message, @NonNull Uri sourceOrigin,
                              boolean isMainFrame, @NonNull JavaScriptReplyProxy replyProxy) {
        if (!isMainFrame || message.getData() == null) return;
        try {
            if (!"take".equals(new JSONObject(message.getData()).optString("op"))) return;
            page = replyProxy;
            JSONObject reply = new JSONObject();
            reply.put("links", new JSONArray(waiting));
            waiting.clear();
            replyProxy.postMessage(reply.toString());
        } catch (Exception ignored) {
            // The links wait for the next ask.
        }
    }

    /** Keeps a leafen: link the app was opened with and tells the page. */
    public void opened(Uri uri) {
        if (uri == null || !SCHEME.equals(uri.getScheme())) return;
        String link = uri.toString();
        if (link.length() > MAX_LINK) return;
        if (waiting.size() >= MAX_WAITING) waiting.remove(0);
        waiting.add(link);
        if (page == null) return;
        try {
            page.postMessage(new JSONObject().put("waiting", true).toString());
        } catch (Exception ignored) {
            // The page takes it when it next asks.
        }
    }
}
