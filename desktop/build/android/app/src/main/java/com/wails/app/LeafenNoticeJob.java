package com.wails.app;

import android.app.Notification;
import android.app.NotificationChannel;
import android.app.NotificationManager;
import android.app.PendingIntent;
import android.app.RemoteInput;
import android.app.job.JobParameters;
import android.app.job.JobService;
import android.content.Context;
import android.content.Intent;
import android.content.SharedPreferences;
import android.net.Uri;
import android.os.Build;

import org.json.JSONArray;
import org.json.JSONObject;

/**
 * The notifications' background job (LeafenNotices): asks the watched server
 * what came since the page last saw — through the Go side, which checks a
 * server identity the page remembers as the page does — shows each as a
 * notification that opens its page, with the unread count, and keeps where
 * to ask after next. A question card's or an approval's notice has its
 * buttons (LeafenNoticeAnswer).
 */
public class LeafenNoticeJob extends JobService {
    static final String CHANNEL = "leafen-notices";

    static {
        System.loadLibrary("wails");
    }

    private static native String nativeFetch(String watch, String token);

    @Override
    public boolean onStartJob(JobParameters params) {
        new Thread(() -> {
            try {
                refresh(getApplicationContext());
            } catch (Throwable ignored) {
                // The next run asks again.
            }
            jobFinished(params, false);
        }).start();
        return true;
    }

    @Override
    public boolean onStopJob(JobParameters params) {
        return true;
    }

    static void refresh(Context context) throws Exception {
        SharedPreferences preferences = context.getSharedPreferences(LeafenNotices.PREFERENCES, Context.MODE_PRIVATE);
        String kept = preferences.getString(LeafenNotices.WATCH, null);
        if (kept == null) return;
        JSONObject watch = new JSONObject(kept);
        String secret = LeafenCredentials.loadFor(context, watch.optString("profile"));
        String token = secret.isEmpty() ? "" : new JSONObject(secret).optString("token");
        if (token.isEmpty()) return;
        JSONObject answer = new JSONObject(nativeFetch(kept, token));
        if (answer.has("error")) return;
        NotificationManager manager = (NotificationManager) context.getSystemService(Context.NOTIFICATION_SERVICE);
        if (manager == null) return;
        int unread = answer.optInt("unreadCount");
        JSONArray notices = answer.optJSONArray("notices");
        for (int index = 0; notices != null && index < notices.length(); index++) {
            show(context, manager, notices.getJSONObject(index), unread);
        }
        watch.put("through", answer.optLong("through", watch.optLong("through")));
        // The page may have watched something else meanwhile; that stands.
        if (kept.equals(preferences.getString(LeafenNotices.WATCH, null))) {
            preferences.edit().putString(LeafenNotices.WATCH, watch.toString()).apply();
        }
    }

    /**
     * Shows one notice, which opens its page; a question card's or an
     * approval's has its buttons and, for a card of one question, a reply,
     * which LeafenNoticeAnswer sends. The same id takes the place of the one shown.
     */
    static void show(Context context, NotificationManager manager, JSONObject notice, int unread) throws Exception {
        if (Build.VERSION.SDK_INT >= 26) {
            manager.createNotificationChannel(new NotificationChannel(CHANNEL,
                    context.getString(R.string.leafen_notices_channel), NotificationManager.IMPORTANCE_DEFAULT));
        }
        String id = notice.optString("id");
        Intent open = new Intent(Intent.ACTION_VIEW, Uri.parse(notice.optString("link")), context, MainActivity.class)
                .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK);
        PendingIntent tap = PendingIntent.getActivity(context, id.hashCode(), open,
                PendingIntent.FLAG_UPDATE_CURRENT | (Build.VERSION.SDK_INT >= 23 ? PendingIntent.FLAG_IMMUTABLE : 0));
        Notification.Builder builder = Build.VERSION.SDK_INT >= 26
                ? new Notification.Builder(context, CHANNEL) : new Notification.Builder(context);
        builder.setSmallIcon(R.drawable.leafen_notice)
                .setContentTitle(notice.optString("title"))
                .setContentText(notice.optString("body"))
                .setStyle(new Notification.BigTextStyle().bigText(notice.optString("body")))
                .setNumber(unread)
                .setAutoCancel(true)
                // A notice taking the place of its own sounds once.
                .setOnlyAlertOnce(true)
                .setContentIntent(tap);
        JSONObject answer = notice.optJSONObject("answer");
        JSONArray buttons = notice.optJSONArray("actions");
        if (answer != null) {
            for (int index = 0; buttons != null && index < buttons.length(); index++) {
                JSONObject button = buttons.getJSONObject(index);
                builder.addAction(new Notification.Action.Builder(0, button.optString("title"),
                        LeafenNoticeAnswer.intent(context, notice, button.optString("action"), false)).build());
            }
            String reply = answer.optString("reply");
            if (!reply.isEmpty() && Build.VERSION.SDK_INT >= 24) {
                // A reply in the notice itself, as a messaging app's.
                RemoteInput words = new RemoteInput.Builder(LeafenNoticeAnswer.TEXT).setLabel(reply).build();
                builder.addAction(new Notification.Action.Builder(0, reply,
                        LeafenNoticeAnswer.intent(context, notice, LeafenNoticeAnswer.REPLY, true)).addRemoteInput(words).build());
            }
        }
        manager.notify(id, 0, builder.build());
    }
}
