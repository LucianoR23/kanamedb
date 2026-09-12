//go:build android && cgo

package secrets

/*
#cgo LDFLAGS: -llog

#include <jni.h>
#include <stdlib.h>
#include <string.h>
#include <android/log.h>

// El puente con dev.kaname.vault.KanameVault. Es lo único de JNI propio del
// proyecto: el resto es de Wails. Todo viaja como byte[] para no depender del
// UTF-8 modificado de NewStringUTF, que no acepta caracteres fuera del BMP.

#define KV_TAG "KanameVault"

static JavaVM*   kv_jvm    = NULL;
static jclass    kv_class  = NULL;
static jmethodID kv_get    = NULL;
static jmethodID kv_set    = NULL;
static jmethodID kv_delete = NULL;
static jmethodID kv_has    = NULL;
static jmethodID kv_throwable_message = NULL;

// JNI_OnLoad corre cuando WailsBridge hace System.loadLibrary("wails"). Wails
// no define la suya, así que es el lugar para quedarse con la JavaVM y resolver
// la clase con el class loader de la app, que es el único que la conoce.
jint JNI_OnLoad(JavaVM* vm, void* reserved) {
    JNIEnv* env = NULL;
    if ((*vm)->GetEnv(vm, (void**)&env, JNI_VERSION_1_6) != JNI_OK) {
        return JNI_ERR;
    }
    kv_jvm = vm;

    jclass cls = (*env)->FindClass(env, "dev/kaname/vault/KanameVault");
    if (cls == NULL) {
        (*env)->ExceptionClear(env);
        __android_log_print(ANDROID_LOG_ERROR, KV_TAG, "no se encontró dev.kaname.vault.KanameVault: las contraseñas no se van a poder guardar");
        return JNI_VERSION_1_6;
    }
    kv_class  = (jclass)(*env)->NewGlobalRef(env, cls);
    kv_get    = (*env)->GetStaticMethodID(env, kv_class, "get",    "([B)[B");
    kv_set    = (*env)->GetStaticMethodID(env, kv_class, "set",    "([B[B)V");
    kv_delete = (*env)->GetStaticMethodID(env, kv_class, "delete", "([B)V");
    kv_has    = (*env)->GetStaticMethodID(env, kv_class, "has",    "([B)Z");

    jclass thr = (*env)->FindClass(env, "java/lang/Throwable");
    kv_throwable_message = (*env)->GetMethodID(env, thr, "getMessage", "()Ljava/lang/String;");

    if ((*env)->ExceptionCheck(env) || kv_get == NULL || kv_set == NULL || kv_delete == NULL || kv_has == NULL) {
        (*env)->ExceptionClear(env);
        __android_log_print(ANDROID_LOG_ERROR, KV_TAG, "KanameVault no tiene los métodos esperados");
        kv_class = NULL;
    }
    return JNI_VERSION_1_6;
}

static JNIEnv* kv_env(int* detach) {
    *detach = 0;
    if (kv_jvm == NULL) return NULL;
    JNIEnv* env = NULL;
    jint r = (*kv_jvm)->GetEnv(kv_jvm, (void**)&env, JNI_VERSION_1_6);
    if (r == JNI_EDETACHED) {
        if ((*kv_jvm)->AttachCurrentThread(kv_jvm, &env, NULL) != 0) return NULL;
        *detach = 1;
    } else if (r != JNI_OK) {
        return NULL;
    }
    return env;
}

static void kv_release(int detach) {
    if (detach && kv_jvm != NULL) (*kv_jvm)->DetachCurrentThread(kv_jvm);
}

// kv_result es lo que vuelve de cada llamada. out/err los libera Go con free.
typedef struct {
    char* out;     // el secreto (get); NULL si no hay o hubo error
    int   out_len;
    int   found;   // get: había entrada; has: existe
    char* err;     // mensaje de la excepción de Java, o NULL
} kv_result;

// kv_take_exception convierte la excepción pendiente en r->err y la limpia.
static void kv_take_exception(JNIEnv* env, kv_result* r) {
    jthrowable ex = (*env)->ExceptionOccurred(env);
    if (ex == NULL) return;
    (*env)->ExceptionClear(env);
    const char* msg = NULL;
    jstring jmsg = NULL;
    if (kv_throwable_message != NULL) {
        jmsg = (jstring)(*env)->CallObjectMethod(env, ex, kv_throwable_message);
        if ((*env)->ExceptionCheck(env)) { (*env)->ExceptionClear(env); jmsg = NULL; }
    }
    if (jmsg != NULL) msg = (*env)->GetStringUTFChars(env, jmsg, NULL);
    r->err = strdup(msg != NULL ? msg : "error sin mensaje en KanameVault");
    if (msg != NULL) (*env)->ReleaseStringUTFChars(env, jmsg, msg);
    if (jmsg != NULL) (*env)->DeleteLocalRef(env, jmsg);
    (*env)->DeleteLocalRef(env, ex);
}

static jbyteArray kv_bytes(JNIEnv* env, const char* p, int n) {
    jbyteArray a = (*env)->NewByteArray(env, n);
    if (a == NULL) return NULL;
    if (n > 0) (*env)->SetByteArrayRegion(env, a, 0, n, (const jbyte*)p);
    return a;
}

// kv_call ejecuta op sobre KanameVault: 0 get, 1 set, 2 delete, 3 has.
static kv_result kv_call(int op, const char* key, int key_len, const char* val, int val_len) {
    kv_result r; memset(&r, 0, sizeof r);
    if (kv_class == NULL) {
        r.err = strdup("el vault de Android no está disponible en este build");
        return r;
    }
    int detach = 0;
    JNIEnv* env = kv_env(&detach);
    if (env == NULL) {
        r.err = strdup("no se pudo obtener el entorno JNI");
        return r;
    }
    if ((*env)->PushLocalFrame(env, 8) < 0) {
        kv_release(detach);
        r.err = strdup("sin memoria para el marco JNI");
        return r;
    }

    jbyteArray jkey = kv_bytes(env, key, key_len);
    jbyteArray jval = NULL;
    if (jkey == NULL) { kv_take_exception(env, &r); goto out; }

    switch (op) {
    case 0: {
        jbyteArray res = (jbyteArray)(*env)->CallStaticObjectMethod(env, kv_class, kv_get, jkey);
        if ((*env)->ExceptionCheck(env)) { kv_take_exception(env, &r); break; }
        if (res == NULL) { r.found = 0; break; }
        jsize n = (*env)->GetArrayLength(env, res);
        r.out = (char*)malloc(n > 0 ? n : 1);
        r.out_len = n;
        if (n > 0) {
            (*env)->GetByteArrayRegion(env, res, 0, n, (jbyte*)r.out);
            // El array de Java no lo necesita nadie más: se pisa antes de
            // soltarlo, para que el secreto no quede a la espera del GC.
            char* zeros = (char*)calloc(n, 1);
            if (zeros != NULL) {
                (*env)->SetByteArrayRegion(env, res, 0, n, (const jbyte*)zeros);
                free(zeros);
            }
        }
        r.found = 1;
        break;
    }
    case 1:
        jval = kv_bytes(env, val, val_len);
        if (jval == NULL) { kv_take_exception(env, &r); break; }
        (*env)->CallStaticVoidMethod(env, kv_class, kv_set, jkey, jval);
        kv_take_exception(env, &r);
        break;
    case 2:
        (*env)->CallStaticVoidMethod(env, kv_class, kv_delete, jkey);
        kv_take_exception(env, &r);
        break;
    case 3:
        r.found = (*env)->CallStaticBooleanMethod(env, kv_class, kv_has, jkey) ? 1 : 0;
        kv_take_exception(env, &r);
        break;
    }

out:
    (*env)->PopLocalFrame(env, NULL);
    kv_release(detach);
    return r;
}
*/
import "C"

import (
	"errors"
	"unsafe"
)

// vault es el backend de secretos de Android: KanameVault.java por JNI. Clave
// AES en el Keystore atada a biometría fuerte, BiometricPrompt con
// CryptoObject en cada cifrado y descifrado. Implementa almacenSeguro, así que
// la composición de claves y sus tests son los mismos que con el bridge falso.
type vault struct{}

// almacenDelSistema en Android es el vault. Si el build no trae la clase Java
// —un APK armado sin build/android/app/src/main/java/dev/kaname/vault—, cada
// llamada falla con un mensaje claro en vez de guardar en claro.
func almacenDelSistema() almacen { return almacenMovil{s: vault{}} }

const (
	opGet = iota
	opSet
	opDelete
	opHas
)

// llamar cruza a Java. key y val se pasan por puntero y longitud; C no los
// retiene. El secreto que vuelve se copia a un string de Go y el buffer de C
// se pisa con ceros antes de liberarse.
func llamar(op int, key, val string) (out string, found bool, err error) {
	var kp, vp *C.char
	if key != "" {
		kp = C.CString(key)
		defer C.free(unsafe.Pointer(kp))
	}
	if val != "" {
		vp = C.CString(val)
		defer func() {
			C.memset(unsafe.Pointer(vp), 0, C.size_t(len(val)))
			C.free(unsafe.Pointer(vp))
		}()
	}
	r := C.kv_call(C.int(op), kp, C.int(len(key)), vp, C.int(len(val)))
	if r.err != nil {
		err = errors.New(C.GoString(r.err))
		C.free(unsafe.Pointer(r.err))
	}
	if r.out != nil {
		out = C.GoStringN(r.out, r.out_len)
		C.memset(unsafe.Pointer(r.out), 0, C.size_t(r.out_len))
		C.free(unsafe.Pointer(r.out))
	}
	return out, r.found != 0, err
}

func (vault) SecureSet(clave, valor string) error {
	_, _, err := llamar(opSet, clave, valor)
	return err
}

func (vault) SecureGet(clave string) (string, bool, error) {
	return llamar(opGet, clave, "")
}

func (vault) SecureDelete(clave string) error {
	_, _, err := llamar(opDelete, clave, "")
	return err
}

func (vault) SecureHas(clave string) (bool, error) {
	_, hay, err := llamar(opHas, clave, "")
	return hay, err
}
