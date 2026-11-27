package com.wails.app;

import android.content.Context;
import android.content.SharedPreferences;
import android.net.Uri;
import android.os.Build;
import android.security.keystore.KeyGenParameterSpec;
import android.security.keystore.KeyProperties;
import android.util.Base64;
import android.webkit.WebView;

import androidx.annotation.NonNull;
import androidx.annotation.RequiresApi;
import androidx.webkit.JavaScriptReplyProxy;
import androidx.webkit.WebMessageCompat;
import androidx.webkit.WebViewCompat;
import androidx.webkit.WebViewFeature;

import org.json.JSONObject;

import java.nio.charset.StandardCharsets;
import java.security.KeyStore;
import java.util.Collections;

import javax.crypto.Cipher;
import javax.crypto.KeyGenerator;
import javax.crypto.SecretKey;
import javax.crypto.spec.GCMParameterSpec;

/**
 * Leafen's sign-ins on other servers, kept for the window as the desktop and
 * iOS apps keep them in their platform's secret storage: each is sealed with
 * an AES key that never leaves the Android Keystore and kept in the app's
 * private preferences. Only the app's own page reaches it, as
 * window.leafenCredentials, never a frame inside the page: requests
 * ({id, op: load|save|forget, profile, secret}) come by postMessage and
 * answers ({id, ok, secret}) go back the same way.
 */
@RequiresApi(api = Build.VERSION_CODES.M)
public class LeafenCredentials implements WebViewCompat.WebMessageListener {
    private static final String KEY_ALIAS = "leafen-profile-credentials";
    private static final String PREFERENCES = "leafen-profile-credentials";
    private static final String PAGE_ORIGIN = "https://wails.localhost";
    private static final int MAX_CHARS = 16 * 1024;

    private final SharedPreferences preferences;

    private LeafenCredentials(Context context) {
        preferences = context.getSharedPreferences(PREFERENCES, Context.MODE_PRIVATE);
    }

    /** Gives the app's page its store, before the page loads. */
    public static void attach(WebView webView, Context context) {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.M
                || !WebViewFeature.isFeatureSupported(WebViewFeature.WEB_MESSAGE_LISTENER)) {
            return;
        }
        WebViewCompat.addWebMessageListener(webView, "leafenCredentials",
                Collections.singleton(PAGE_ORIGIN), new LeafenCredentials(context));
    }

    @Override
    public void onPostMessage(@NonNull WebView view, @NonNull WebMessageCompat message, @NonNull Uri sourceOrigin,
                              boolean isMainFrame, @NonNull JavaScriptReplyProxy replyProxy) {
        if (!isMainFrame || message.getData() == null) return;
        try {
            JSONObject request = new JSONObject(message.getData());
            String profile = request.optString("profile");
            JSONObject reply = new JSONObject().put("id", request.optString("id"));
            switch (request.optString("op")) {
                case "load":
                    reply.put("ok", validProfile(profile)).put("secret", load(profile));
                    break;
                case "save":
                    reply.put("ok", save(profile, request.optString("secret")));
                    break;
                case "forget":
                    reply.put("ok", forget(profile));
                    break;
                default:
                    reply.put("ok", false);
            }
            replyProxy.postMessage(reply.toString());
        } catch (Exception error) {
            // A message that isn't a request has no answer.
        }
    }

    /** A server's origin, or "local", the app's own server. */
    private static boolean validProfile(String profile) {
        return profile != null && profile.length() <= 512
                && (profile.startsWith("https://") || profile.startsWith("http://") || profile.equals("local")
                    || profile.equals("device-models"));
    }

    private static SecretKey key() throws Exception {
        KeyStore keyStore = KeyStore.getInstance("AndroidKeyStore");
        keyStore.load(null);
        if (keyStore.containsAlias(KEY_ALIAS)) {
            return ((KeyStore.SecretKeyEntry) keyStore.getEntry(KEY_ALIAS, null)).getSecretKey();
        }
        KeyGenerator generator = KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, "AndroidKeyStore");
        generator.init(new KeyGenParameterSpec.Builder(KEY_ALIAS,
                KeyProperties.PURPOSE_ENCRYPT | KeyProperties.PURPOSE_DECRYPT)
                .setBlockModes(KeyProperties.BLOCK_MODE_GCM)
                .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
                .setKeySize(256)
                .build());
        return generator.generateKey();
    }

    /** A profile's kept sign-in for the app's own background work (LeafenNoticeJob). */
    static String loadFor(Context context, String profile) {
        return new LeafenCredentials(context).load(profile);
    }

    /** A profile's kept sign-in, or "" when none is kept or it can't be read. */
    private String load(String profile) {
        if (!validProfile(profile)) return "";
        String sealed = preferences.getString(profile, null);
        if (sealed == null) return "";
        try {
            byte[] bytes = Base64.decode(sealed, Base64.NO_WRAP);
            Cipher cipher = Cipher.getInstance("AES/GCM/NoPadding");
            cipher.init(Cipher.DECRYPT_MODE, key(), new GCMParameterSpec(128, bytes, 0, 12));
            return new String(cipher.doFinal(bytes, 12, bytes.length - 12), StandardCharsets.UTF_8);
        } catch (Exception error) {
            return "";
        }
    }

    /** Keeps a profile's sign-in in place of the one kept before; "" drops it. */
    private boolean save(String profile, String secret) {
        if (!validProfile(profile) || secret == null || secret.length() > MAX_CHARS) return false;
        if (secret.isEmpty()) return forget(profile);
        try {
            Cipher cipher = Cipher.getInstance("AES/GCM/NoPadding");
            cipher.init(Cipher.ENCRYPT_MODE, key());
            byte[] iv = cipher.getIV();
            byte[] sealed = cipher.doFinal(secret.getBytes(StandardCharsets.UTF_8));
            byte[] bytes = new byte[iv.length + sealed.length];
            System.arraycopy(iv, 0, bytes, 0, iv.length);
            System.arraycopy(sealed, 0, bytes, iv.length, sealed.length);
            return preferences.edit().putString(profile, Base64.encodeToString(bytes, Base64.NO_WRAP)).commit();
        } catch (Exception error) {
            return false;
        }
    }

    /** Drops a profile's kept sign-in. */
    private boolean forget(String profile) {
        if (!validProfile(profile)) return false;
        return preferences.edit().remove(profile).commit();
    }
}
