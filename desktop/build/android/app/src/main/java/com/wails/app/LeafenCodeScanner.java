package com.wails.app;

import android.app.Activity;
import android.content.Intent;
import android.content.pm.PackageManager;
import android.net.Uri;
import android.os.Build;
import android.webkit.WebView;

import androidx.annotation.NonNull;
import androidx.webkit.JavaScriptReplyProxy;
import androidx.webkit.WebMessageCompat;
import androidx.webkit.WebViewCompat;
import androidx.webkit.WebViewFeature;

import org.json.JSONObject;

import java.util.Collections;

/**
 * Reads a QR code with the camera for the app's page (lib/codeScanner.ts):
 * the server address another device shows, to connect to it at the app's
 * first start. The camera is asked for only when the person chooses to scan
 * (LeafenCodeScanActivity shows it). Only the app's own page reaches it, as
 * window.leafenCodeScanner: a request ({id, op: "scan"}) comes by
 * postMessage and the answer ({id, outcome: scanned|cancelled|denied|
 * unavailable, text}) goes back.
 */
public final class LeafenCodeScanner implements WebViewCompat.WebMessageListener {
    private static final String PAGE_ORIGIN = "https://wails.localhost";
    static final int SCAN_REQUEST = 0x1eb1;
    static final int CAMERA_REQUEST = 0x1eb2;

    private final Activity activity;
    private JavaScriptReplyProxy waiting;
    private String waitingId = "";

    private LeafenCodeScanner(Activity activity) {
        this.activity = activity;
    }

    /** Gives the app's page its scanner, before the page loads. */
    public static LeafenCodeScanner attach(WebView webView, Activity activity) {
        LeafenCodeScanner scanner = new LeafenCodeScanner(activity);
        if (WebViewFeature.isFeatureSupported(WebViewFeature.WEB_MESSAGE_LISTENER)) {
            WebViewCompat.addWebMessageListener(webView, "leafenCodeScanner",
                    Collections.singleton(PAGE_ORIGIN), scanner);
        }
        return scanner;
    }

    @Override
    public void onPostMessage(@NonNull WebView view, @NonNull WebMessageCompat message, @NonNull Uri sourceOrigin,
                              boolean isMainFrame, @NonNull JavaScriptReplyProxy replyProxy) {
        if (!isMainFrame || message.getData() == null) return;
        String id;
        try {
            JSONObject request = new JSONObject(message.getData());
            if (!"scan".equals(request.optString("op"))) return;
            id = request.optString("id");
        } catch (Exception error) {
            return;
        }
        // A scan asked again ends the one before.
        answer("cancelled", "");
        waiting = replyProxy;
        waitingId = id;
        if (!activity.getPackageManager().hasSystemFeature(PackageManager.FEATURE_CAMERA_ANY)) {
            answer("unavailable", "");
        } else if (Build.VERSION.SDK_INT < 23
                || activity.checkSelfPermission(android.Manifest.permission.CAMERA) == PackageManager.PERMISSION_GRANTED) {
            show();
        } else {
            activity.requestPermissions(new String[]{android.Manifest.permission.CAMERA}, CAMERA_REQUEST);
        }
    }

    private void show() {
        activity.startActivityForResult(new Intent(activity, LeafenCodeScanActivity.class), SCAN_REQUEST);
    }

    /** The camera allowed or refused; false when the request was another's. */
    boolean onPermissionResult(int requestCode, int[] grants) {
        if (requestCode != CAMERA_REQUEST) return false;
        if (grants.length > 0 && grants[0] == PackageManager.PERMISSION_GRANTED) show();
        else answer("denied", "");
        return true;
    }

    /** What the scanner read; false when the result was another's. */
    boolean onActivityResult(int requestCode, int resultCode, Intent data) {
        if (requestCode != SCAN_REQUEST) return false;
        String text = data != null ? data.getStringExtra(LeafenCodeScanActivity.EXTRA_TEXT) : null;
        if (resultCode == Activity.RESULT_OK && text != null && !text.isEmpty()) answer("scanned", text);
        else if (resultCode == LeafenCodeScanActivity.RESULT_UNAVAILABLE) answer("unavailable", "");
        else answer("cancelled", "");
        return true;
    }

    private void answer(String outcome, String text) {
        JavaScriptReplyProxy reply = waiting;
        String id = waitingId;
        waiting = null;
        waitingId = "";
        if (reply == null) return;
        try {
            JSONObject answer = new JSONObject();
            answer.put("id", id);
            answer.put("outcome", outcome);
            answer.put("text", text);
            reply.postMessage(answer.toString());
        } catch (Exception error) {
            // The page is gone.
        }
    }
}
