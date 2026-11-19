//go:build android

package main

/*
#include <jni.h>
#include <stdint.h>

static int enabled(uintptr_t env, uintptr_t context) {
	JNIEnv *e = (JNIEnv*)env;
	jobject activity = (jobject)context;
	jclass version = (*e)->FindClass(e, "android/os/Build$VERSION");
	if (version) {
		jfieldID sdk = (*e)->GetStaticFieldID(e, version, "SDK_INT", "I");
		if (sdk && (*e)->GetStaticIntField(e, version, sdk) < 24) return 1;
	}
	if ((*e)->ExceptionCheck(e)) (*e)->ExceptionClear(e);
	jclass activityType = (*e)->GetObjectClass(e, activity);
	jmethodID service = (*e)->GetMethodID(e, activityType, "getSystemService", "(Ljava/lang/String;)Ljava/lang/Object;");
	if (!service) { (*e)->ExceptionClear(e); return 0; }
	jstring key = (*e)->NewStringUTF(e, "notification");
	jobject manager = (*e)->CallObjectMethod(e, activity, service, key);
	if ((*e)->ExceptionCheck(e) || !manager) { (*e)->ExceptionClear(e); return 0; }
	jmethodID enabled = (*e)->GetMethodID(e, (*e)->GetObjectClass(e, manager), "areNotificationsEnabled", "()Z");
	if (!enabled) { (*e)->ExceptionClear(e); return 0; }
	jboolean result = (*e)->CallBooleanMethod(e, manager, enabled);
	if ((*e)->ExceptionCheck(e)) { (*e)->ExceptionClear(e); return 0; }
	return result == JNI_TRUE;
}

static int open_settings(uintptr_t env, uintptr_t context) {
	JNIEnv *e = (JNIEnv*)env;
	jobject activity = (jobject)context;
	jclass activityType = (*e)->GetObjectClass(e, activity);
	jclass intentType = (*e)->FindClass(e, "android/content/Intent");
	if (!intentType) { (*e)->ExceptionClear(e); return 0; }
	jmethodID init = (*e)->GetMethodID(e, intentType, "<init>", "(Ljava/lang/String;)V");
	jmethodID extra = (*e)->GetMethodID(e, intentType, "putExtra", "(Ljava/lang/String;Ljava/lang/String;)Landroid/content/Intent;");
	jmethodID flags = (*e)->GetMethodID(e, intentType, "addFlags", "(I)Landroid/content/Intent;");
	jmethodID pkg = (*e)->GetMethodID(e, activityType, "getPackageName", "()Ljava/lang/String;");
	jmethodID start = (*e)->GetMethodID(e, activityType, "startActivity", "(Landroid/content/Intent;)V");
	if (!init || !extra || !flags || !pkg || !start) { (*e)->ExceptionClear(e); return 0; }
	jobject intent = (*e)->NewObject(e, intentType, init, (*e)->NewStringUTF(e, "android.settings.APP_NOTIFICATION_SETTINGS"));
	jobject name = (*e)->CallObjectMethod(e, activity, pkg);
	(*e)->CallObjectMethod(e, intent, extra, (*e)->NewStringUTF(e, "android.provider.extra.APP_PACKAGE"), name);
	(*e)->CallObjectMethod(e, intent, flags, (jint)0x10000000);
	if ((*e)->ExceptionCheck(e)) { (*e)->ExceptionClear(e); return 0; }
	(*e)->CallVoidMethod(e, activity, start, intent);
	if ((*e)->ExceptionCheck(e)) { (*e)->ExceptionClear(e); return 0; }
	return 1;
}
*/
import "C"

import "fyne.io/fyne/v2/driver"

func notificationsEnabled() bool {
	allowed := false
	_ = driver.RunNative(func(raw any) error {
		if android, ok := raw.(*driver.AndroidContext); ok && android.Env != 0 && android.Ctx != 0 {
			allowed = C.enabled(C.uintptr_t(android.Env), C.uintptr_t(android.Ctx)) != 0
		}
		return nil
	})
	return allowed
}

func openNotificationSettings() bool {
	opened := false
	_ = driver.RunNative(func(raw any) error {
		if android, ok := raw.(*driver.AndroidContext); ok && android.Env != 0 && android.Ctx != 0 {
			opened = C.open_settings(C.uintptr_t(android.Env), C.uintptr_t(android.Ctx)) != 0
		}
		return nil
	})
	return opened
}
