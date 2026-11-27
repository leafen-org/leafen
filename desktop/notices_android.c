//go:build android

#include <jni.h>
#include <stdlib.h>

// The background job (LeafenNoticeJob.java) asks the server through the Go
// side (notices_android.go), which checks a remembered server identity as
// the window does; a notice's button (LeafenNoticeAnswer.java) answers the
// same way.

extern char *leafenAndroidFetchNotices(char *watch, char *token);
extern char *leafenAndroidAnswerNotice(char *notice, char *action, char *text);

JNIEXPORT jstring JNICALL Java_com_wails_app_LeafenNoticeJob_nativeFetch(JNIEnv *env, jclass cls, jstring watch, jstring token) {
	const char *watchText = watch != NULL ? (*env)->GetStringUTFChars(env, watch, NULL) : NULL;
	const char *tokenText = token != NULL ? (*env)->GetStringUTFChars(env, token, NULL) : NULL;
	char *answer = leafenAndroidFetchNotices((char *)(watchText != NULL ? watchText : ""), (char *)(tokenText != NULL ? tokenText : ""));
	if (watchText != NULL) (*env)->ReleaseStringUTFChars(env, watch, watchText);
	if (tokenText != NULL) (*env)->ReleaseStringUTFChars(env, token, tokenText);
	jstring result = (*env)->NewStringUTF(env, answer);
	free(answer);
	return result;
}

JNIEXPORT jstring JNICALL Java_com_wails_app_LeafenNoticeAnswer_nativeAnswer(JNIEnv *env, jclass cls, jstring notice, jstring action, jstring text) {
	const char *noticeText = notice != NULL ? (*env)->GetStringUTFChars(env, notice, NULL) : NULL;
	const char *actionText = action != NULL ? (*env)->GetStringUTFChars(env, action, NULL) : NULL;
	const char *words = text != NULL ? (*env)->GetStringUTFChars(env, text, NULL) : NULL;
	char *answer = leafenAndroidAnswerNotice((char *)(noticeText != NULL ? noticeText : ""), (char *)(actionText != NULL ? actionText : ""),
		(char *)(words != NULL ? words : ""));
	if (noticeText != NULL) (*env)->ReleaseStringUTFChars(env, notice, noticeText);
	if (actionText != NULL) (*env)->ReleaseStringUTFChars(env, action, actionText);
	if (words != NULL) (*env)->ReleaseStringUTFChars(env, text, words);
	jstring result = (*env)->NewStringUTF(env, answer);
	free(answer);
	return result;
}
