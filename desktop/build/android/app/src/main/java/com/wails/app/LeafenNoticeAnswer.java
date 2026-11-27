package com.wails.app;

import android.app.NotificationManager;
import android.app.PendingIntent;
import android.app.RemoteInput;
import android.content.BroadcastReceiver;
import android.content.Context;
import android.content.Intent;
import android.os.Build;
import android.os.Bundle;

import org.json.JSONObject;

/**
 * A notice's button or reply pressed (LeafenNoticeJob): the Go side sends it
 * to the notice's server, checking a server identity the page remembers as
 * the page does, without bringing the app forward. Answered, the notice goes
 * (a reply's stays with what was sent); a card no longer waited on says so in
 * its place; an answer that couldn't go leaves the notice, without its
 * buttons, to open the chat.
 */
public class LeafenNoticeAnswer extends BroadcastReceiver {
    static final String TEXT = "text";
    static final String REPLY = "reply";
    private static final String NOTICE = "notice";
    private static final String ACTION = "action";

    static {
        System.loadLibrary("wails");
    }

    private static native String nativeAnswer(String notice, String action, String text);

    /** What a notice's button sends; a reply's takes the words written in it. */
    static PendingIntent intent(Context context, JSONObject notice, String action, boolean reply) {
        Intent answer = new Intent(context, LeafenNoticeAnswer.class)
                .putExtra(NOTICE, notice.toString())
                .putExtra(ACTION, action);
        int flags = PendingIntent.FLAG_UPDATE_CURRENT;
        if (reply && Build.VERSION.SDK_INT >= 31) {
            flags |= PendingIntent.FLAG_MUTABLE;
        } else if (!reply && Build.VERSION.SDK_INT >= 23) {
            flags |= PendingIntent.FLAG_IMMUTABLE;
        }
        return PendingIntent.getBroadcast(context, (notice.optString("id") + "\u0000" + action).hashCode(), answer, flags);
    }

    @Override
    public void onReceive(Context context, Intent intent) {
        String notice = intent.getStringExtra(NOTICE);
        String action = intent.getStringExtra(ACTION);
        if (notice == null || action == null) return;
        Bundle words = RemoteInput.getResultsFromIntent(intent);
        CharSequence text = words != null ? words.getCharSequence(TEXT) : null;
        Context app = context.getApplicationContext();
        PendingResult pending = goAsync();
        new Thread(() -> {
            try {
                NotificationManager manager = (NotificationManager) app.getSystemService(Context.NOTIFICATION_SERVICE);
                JSONObject after = new JSONObject(nativeAnswer(notice, action, text == null ? "" : text.toString()));
                JSONObject shown = after.optJSONObject(NOTICE);
                JSONObject kept = new JSONObject(notice);
                if (manager == null) return;
                if (shown != null) {
                    LeafenNoticeJob.show(app, manager, shown, 0);
                } else if (REPLY.equals(action)) {
                    // A reply answered stays, without its buttons, showing
                    // what was sent, as Android asks of a reply in a notice.
                    kept.remove("actions");
                    kept.remove("answer");
                    LeafenNoticeJob.show(app, manager, kept, 0);
                } else {
                    manager.cancel(kept.optString("id"), 0);
                }
            } catch (Throwable ignored) {
                // The notice stays and opens the chat.
            } finally {
                pending.finish();
            }
        }).start();
    }
}
