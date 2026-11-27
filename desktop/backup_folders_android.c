//go:build android

#include <jni.h>
#include <stdlib.h>
#include <string.h>

// The server (Go) reaches the app's backup folder helper
// (LeafenBackupFolders.java) through here: the helper hands over the JVM and
// its class when the activity starts, and Go calls its static methods from
// its own threads, attached for the call.

extern void leafenAndroidFolderChosen(int callbackID, char *tree, char *name);

static JavaVM *leafenVM = NULL;
static jclass leafenFolders = NULL;

JNIEXPORT void JNICALL Java_com_wails_app_LeafenBackupFolders_nativeAttach(JNIEnv *env, jclass cls) {
	(*env)->GetJavaVM(env, &leafenVM);
	if (leafenFolders == NULL) leafenFolders = (jclass)(*env)->NewGlobalRef(env, cls);
}

JNIEXPORT void JNICALL Java_com_wails_app_LeafenBackupFolders_nativeChosen(JNIEnv *env, jclass cls, jint callbackID,
		jstring tree, jstring name) {
	const char *treeText = tree != NULL ? (*env)->GetStringUTFChars(env, tree, NULL) : NULL;
	const char *nameText = name != NULL ? (*env)->GetStringUTFChars(env, name, NULL) : NULL;
	leafenAndroidFolderChosen((int)callbackID, (char *)(treeText != NULL ? treeText : ""), (char *)(nameText != NULL ? nameText : ""));
	if (treeText != NULL) (*env)->ReleaseStringUTFChars(env, tree, treeText);
	if (nameText != NULL) (*env)->ReleaseStringUTFChars(env, name, nameText);
}

static JNIEnv *leafenEnv(int *attached) {
	JNIEnv *env = NULL;
	*attached = 0;
	if (leafenVM == NULL || leafenFolders == NULL) return NULL;
	jint state = (*leafenVM)->GetEnv(leafenVM, (void **)&env, JNI_VERSION_1_6);
	if (state == JNI_EDETACHED) {
		if ((*leafenVM)->AttachCurrentThread(leafenVM, &env, NULL) != 0) return NULL;
		*attached = 1;
	} else if (state != JNI_OK) {
		return NULL;
	}
	return env;
}

static void leafenDone(JNIEnv *env, int attached) {
	if ((*env)->ExceptionCheck(env)) (*env)->ExceptionClear(env);
	if (attached) (*leafenVM)->DetachCurrentThread(leafenVM);
}

int leafenFoldersReady(void) {
	return leafenVM != NULL && leafenFolders != NULL;
}

void leafenFoldersChoose(int callbackID) {
	int attached;
	JNIEnv *env = leafenEnv(&attached);
	if (env == NULL) {
		leafenAndroidFolderChosen(callbackID, "", "");
		return;
	}
	jmethodID method = (*env)->GetStaticMethodID(env, leafenFolders, "choose", "(I)V");
	if (method != NULL) (*env)->CallStaticVoidMethod(env, leafenFolders, method, (jint)callbackID);
	int failed = method == NULL || (*env)->ExceptionCheck(env);
	leafenDone(env, attached);
	if (failed) leafenAndroidFolderChosen(callbackID, "", "");
}

// stat and list: a JSON answer the caller frees, or NULL.
char *leafenFoldersText(const char *name, const char *tree, const char *path) {
	int attached;
	JNIEnv *env = leafenEnv(&attached);
	if (env == NULL) return NULL;
	char *answer = NULL;
	jmethodID method = (*env)->GetStaticMethodID(env, leafenFolders, name, "(Ljava/lang/String;Ljava/lang/String;)Ljava/lang/String;");
	if (method != NULL) {
		jstring treeValue = (*env)->NewStringUTF(env, tree);
		jstring pathValue = (*env)->NewStringUTF(env, path);
		jstring result = (jstring)(*env)->CallStaticObjectMethod(env, leafenFolders, method, treeValue, pathValue);
		if (!(*env)->ExceptionCheck(env) && result != NULL) {
			const char *text = (*env)->GetStringUTFChars(env, result, NULL);
			if (text != NULL) {
				answer = strdup(text);
				(*env)->ReleaseStringUTFChars(env, result, text);
			}
		}
		if (result != NULL) (*env)->DeleteLocalRef(env, result);
		(*env)->DeleteLocalRef(env, treeValue);
		(*env)->DeleteLocalRef(env, pathValue);
	}
	leafenDone(env, attached);
	return answer;
}

// makeDir, removeAll (two paths empty for the second) and rename: 1 when done.
int leafenFoldersAct(const char *name, const char *tree, const char *path, const char *target) {
	int attached;
	JNIEnv *env = leafenEnv(&attached);
	if (env == NULL) return 0;
	jboolean done = JNI_FALSE;
	int renaming = target != NULL;
	jmethodID method = (*env)->GetStaticMethodID(env, leafenFolders, name, renaming
		? "(Ljava/lang/String;Ljava/lang/String;Ljava/lang/String;)Z" : "(Ljava/lang/String;Ljava/lang/String;)Z");
	if (method != NULL) {
		jstring treeValue = (*env)->NewStringUTF(env, tree);
		jstring pathValue = (*env)->NewStringUTF(env, path);
		jstring targetValue = renaming ? (*env)->NewStringUTF(env, target) : NULL;
		done = renaming
			? (*env)->CallStaticBooleanMethod(env, leafenFolders, method, treeValue, pathValue, targetValue)
			: (*env)->CallStaticBooleanMethod(env, leafenFolders, method, treeValue, pathValue);
		if ((*env)->ExceptionCheck(env)) done = JNI_FALSE;
		(*env)->DeleteLocalRef(env, treeValue);
		(*env)->DeleteLocalRef(env, pathValue);
		if (targetValue != NULL) (*env)->DeleteLocalRef(env, targetValue);
	}
	leafenDone(env, attached);
	return done == JNI_TRUE;
}

// openWrite and openRead: a file descriptor the caller owns, or a negative
// answer (-1 missing, -2 there already, -3 failed).
int leafenFoldersOpen(const char *name, const char *tree, const char *path, int create) {
	int attached;
	JNIEnv *env = leafenEnv(&attached);
	if (env == NULL) return -3;
	jint descriptor = -3;
	int writing = strcmp(name, "openWrite") == 0;
	jmethodID method = (*env)->GetStaticMethodID(env, leafenFolders, name, writing
		? "(Ljava/lang/String;Ljava/lang/String;Z)I" : "(Ljava/lang/String;Ljava/lang/String;)I");
	if (method != NULL) {
		jstring treeValue = (*env)->NewStringUTF(env, tree);
		jstring pathValue = (*env)->NewStringUTF(env, path);
		descriptor = writing
			? (*env)->CallStaticIntMethod(env, leafenFolders, method, treeValue, pathValue, create ? JNI_TRUE : JNI_FALSE)
			: (*env)->CallStaticIntMethod(env, leafenFolders, method, treeValue, pathValue);
		if ((*env)->ExceptionCheck(env)) descriptor = -3;
		(*env)->DeleteLocalRef(env, treeValue);
		(*env)->DeleteLocalRef(env, pathValue);
	}
	leafenDone(env, attached);
	return (int)descriptor;
}
