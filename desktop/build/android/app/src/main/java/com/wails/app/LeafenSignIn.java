package com.wails.app;

import android.app.Activity;
import android.content.Intent;
import android.net.Uri;
import android.webkit.WebView;

import androidx.annotation.NonNull;
import androidx.webkit.JavaScriptReplyProxy;
import androidx.webkit.WebMessageCompat;
import androidx.webkit.WebViewCompat;
import androidx.webkit.WebViewFeature;

import org.json.JSONObject;

import java.security.SecureRandom;
import java.util.Collections;

/**
 * Brings a provider sign-in on another Leafen server back to the app's page,
 * as RFC 8252 has native apps do: the page has the system's browser open the
 * server's sign-in, which ends at the app's own scheme; Android opens the app
 * with it, and the page waiting gets the query it carried (a completion code
 * that only the page's PKCE verifier exchanges). Only the app's own page
 * reaches it, as window.leafenSignIn, never a frame inside the page: requests
 * ({id, op: begin|open|wait|cancel, signIn, url}) come by postMessage and
 * answers ({id, ok, signIn, address, query}) go back.
 */
public class LeafenSignIn implements WebViewCompat.WebMessageListener {
    private static final String PAGE_ORIGIN = "https://wails.localhost";
    static final String SCHEME = "org.leafen.app";
    private static final String PATH = "/sign-in/";

    private final Activity activity;
    // The sign-in begun, the page's request waiting for it, and a return that
    // came before the page asked for it.
    private String signIn = "";
    private JavaScriptReplyProxy waitingReply;
    private String waitingRequest = "";
    private String returnedQuery;

    private LeafenSignIn(Activity activity) {
        this.activity = activity;
    }

    /** Gives the app's page its sign-in return, before the page loads. */
    public static LeafenSignIn attach(WebView webView, Activity activity) {
        LeafenSignIn signIn = new LeafenSignIn(activity);
        if (WebViewFeature.isFeatureSupported(WebViewFeature.WEB_MESSAGE_LISTENER)) {
            WebViewCompat.addWebMessageListener(webView, "leafenSignIn",
                    Collections.singleton(PAGE_ORIGIN), signIn);
        }
        return signIn;
    }

    private static String newSignIn() {
        byte[] bytes = new byte[16];
        new SecureRandom().nextBytes(bytes);
        StringBuilder hex = new StringBuilder();
        for (byte value : bytes) hex.append(String.format("%02x", value));
        return hex.toString();
    }

    @Override
    public void onPostMessage(@NonNull WebView view, @NonNull WebMessageCompat message, @NonNull Uri sourceOrigin,
                              boolean isMainFrame, @NonNull JavaScriptReplyProxy replyProxy) {
        if (!isMainFrame || message.getData() == null) return;
        JSONObject reply = new JSONObject();
        try {
            JSONObject request = new JSONObject(message.getData());
            String id = request.optString("id");
            reply.put("id", id);
            switch (request.optString("op")) {
                case "begin":
                    finish("");
                    signIn = newSignIn();
                    returnedQuery = null;
                    reply.put("signIn", signIn);
                    reply.put("address", SCHEME + ":" + PATH + signIn);
                    reply.put("ok", true);
                    break;
                case "open":
                    Uri address = Uri.parse(request.optString("url"));
                    String scheme = address.getScheme();
                    if (!signIn.equals(request.optString("signIn")) || address.getHost() == null
                            || !("https".equals(scheme) || "http".equals(scheme))) {
                        reply.put("ok", false);
                        break;
                    }
                    Intent browser = new Intent(Intent.ACTION_VIEW, address);
                    browser.addCategory(Intent.CATEGORY_BROWSABLE);
                    activity.startActivity(browser);
                    reply.put("ok", true);
                    break;
                case "wait":
                    if (!signIn.equals(request.optString("signIn")) || signIn.isEmpty()) {
                        reply.put("query", "");
                        reply.put("ok", true);
                        break;
                    }
                    if (returnedQuery != null) {
                        reply.put("query", returnedQuery);
                        reply.put("ok", true);
                        signIn = "";
                        returnedQuery = null;
                        break;
                    }
                    finishWaiting("");
                    waitingReply = replyProxy;
                    waitingRequest = id;
                    return;
                case "cancel":
                    if (signIn.equals(request.optString("signIn"))) finish("");
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

    /** Takes the app opened at its scheme: a sign-in's return, if it is one. */
    public boolean returned(Uri uri) {
        if (uri == null || !SCHEME.equals(uri.getScheme())) return false;
        String path = uri.getPath();
        if (path == null || !path.startsWith(PATH) || signIn.isEmpty()
                || !signIn.equals(path.substring(PATH.length()))) {
            return true;
        }
        String query = uri.getEncodedQuery() == null ? "" : uri.getEncodedQuery();
        if (waitingReply == null) {
            returnedQuery = query;
            return true;
        }
        finish(query);
        return true;
    }

    private void finish(String query) {
        finishWaiting(query);
        signIn = "";
        returnedQuery = null;
    }

    private void finishWaiting(String query) {
        if (waitingReply == null) return;
        try {
            JSONObject reply = new JSONObject();
            reply.put("id", waitingRequest);
            reply.put("query", query);
            reply.put("ok", true);
            waitingReply.postMessage(reply.toString());
        } catch (Exception ignored) {
            // The page's wait ends without an answer.
        }
        waitingReply = null;
        waitingRequest = "";
    }
}
