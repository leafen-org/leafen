package com.wails.app;

import android.app.Activity;
import android.app.job.JobInfo;
import android.app.job.JobScheduler;
import android.content.ComponentName;
import android.content.Context;
import android.content.pm.PackageManager;
import android.net.Uri;
import android.os.Build;
import android.webkit.WebView;

import androidx.annotation.NonNull;
import androidx.core.app.NotificationManagerCompat;
import androidx.webkit.JavaScriptReplyProxy;
import androidx.webkit.WebMessageCompat;
import androidx.webkit.WebViewCompat;
import androidx.webkit.WebViewFeature;

import org.json.JSONArray;
import org.json.JSONObject;

import java.util.Collections;

/**
 * Leafen's notifications on Android without push (Leafen runs no push relay,
 * decided 2026-10-19): the app's page says what it watched — the server, its
 * workspaces, what it has seen up to and the identity it remembers there —
 * and a job the system runs about every fifteen minutes (LeafenNoticeJob)
 * asks that server what came since, shows each as a notification and puts the
 * unread count on them. Only the app's own page reaches it, as
 * window.leafenNotices: requests ({id, op: allowed|allow|watch, ...}) come by
 * postMessage and answers ({id, ok, allowed}) go back the same way.
 */
public class LeafenNotices implements WebViewCompat.WebMessageListener {
    static final String PREFERENCES = "leafen-notices";
    static final String WATCH = "watch";
    static final int JOB_ID = 0x1eaf;
    static final int PERMISSION_REQUEST = 0x1eb0;
    private static final String PAGE_ORIGIN = "https://wails.localhost";
    private static final long EVERY_MS = 15 * 60 * 1000L;

    private final Activity activity;
    private JavaScriptReplyProxy asking;
    private String askingId;

    private LeafenNotices(Activity activity) {
        this.activity = activity;
    }

    /** Gives the app's page its notifications, before the page loads. */
    public static LeafenNotices attach(WebView webView, Activity activity) {
        LeafenNotices notices = new LeafenNotices(activity);
        if (WebViewFeature.isFeatureSupported(WebViewFeature.WEB_MESSAGE_LISTENER)) {
            WebViewCompat.addWebMessageListener(webView, "leafenNotices",
                    Collections.singleton(PAGE_ORIGIN), notices);
        }
        return notices;
    }

    @Override
    public void onPostMessage(@NonNull WebView view, @NonNull WebMessageCompat message, @NonNull Uri sourceOrigin,
                              boolean isMainFrame, @NonNull JavaScriptReplyProxy replyProxy) {
        if (!isMainFrame || message.getData() == null) return;
        try {
            JSONObject request = new JSONObject(message.getData());
            String id = request.optString("id");
            switch (request.optString("op")) {
                case "allowed":
                    answer(replyProxy, id, true);
                    break;
                case "allow":
                    if (allowed() || Build.VERSION.SDK_INT < 33) {
                        answer(replyProxy, id, true);
                    } else {
                        // The system asks the person; the answer comes to onPermissionResult.
                        asking = replyProxy;
                        askingId = id;
                        activity.requestPermissions(new String[]{"android.permission.POST_NOTIFICATIONS"}, PERMISSION_REQUEST);
                    }
                    break;
                case "watch":
                    watch(activity, request.optString("profile"), request.optJSONArray("workspaces"),
                            request.optLong("through"), request.optString("identity"));
                    answer(replyProxy, id, true);
                    break;
                default:
                    replyProxy.postMessage(new JSONObject().put("id", id).put("ok", false).toString());
            }
        } catch (Exception error) {
            // A message that isn't a request has no answer.
        }
    }

    /** Takes the person's answer to the notifications permission; false for another request. */
    public boolean onPermissionResult(int requestCode) {
        if (requestCode != PERMISSION_REQUEST) return false;
        if (asking != null) {
            try {
                answer(asking, askingId, true);
            } catch (Exception ignored) {
                // The page asks again when it next reads the setting.
            }
            asking = null;
        }
        return true;
    }

    private void answer(JavaScriptReplyProxy page, String id, boolean ok) throws Exception {
        page.postMessage(new JSONObject().put("id", id).put("ok", ok).put("allowed", allowed()).toString());
    }

    private boolean allowed() {
        if (Build.VERSION.SDK_INT >= 33
                && activity.checkSelfPermission("android.permission.POST_NOTIFICATIONS") != PackageManager.PERMISSION_GRANTED) {
            return false;
        }
        return NotificationManagerCompat.from(activity).areNotificationsEnabled();
    }

    /** Keeps what the page watched and runs the job; an empty profile stops both. */
    static void watch(Context context, String profile, JSONArray workspaces, long through, String identity) throws Exception {
        JobScheduler jobs = (JobScheduler) context.getSystemService(Context.JOB_SCHEDULER_SERVICE);
        if (profile == null || profile.isEmpty() || workspaces == null || workspaces.length() == 0
                || !(profile.startsWith("https://") || profile.startsWith("http://"))) {
            context.getSharedPreferences(PREFERENCES, Context.MODE_PRIVATE).edit().remove(WATCH).apply();
            if (jobs != null) jobs.cancel(JOB_ID);
            return;
        }
        JSONObject watch = new JSONObject().put("profile", profile).put("workspaces", workspaces)
                .put("through", through).put("identity", identity == null ? "" : identity);
        context.getSharedPreferences(PREFERENCES, Context.MODE_PRIVATE).edit().putString(WATCH, watch.toString()).apply();
        if (jobs == null) return;
        for (JobInfo pending : jobs.getAllPendingJobs()) {
            if (pending.getId() == JOB_ID) return;
        }
        jobs.schedule(new JobInfo.Builder(JOB_ID, new ComponentName(context, LeafenNoticeJob.class))
                .setPeriodic(EVERY_MS)
                .setRequiredNetworkType(JobInfo.NETWORK_TYPE_ANY)
                .setPersisted(true)
                .build());
    }
}
