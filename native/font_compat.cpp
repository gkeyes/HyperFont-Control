#include "third_party/jni.h"

// Zygisk API uses the 64-bit Android dev_t/ino_t ABI. Only these ABIs are packaged.
using dev_t = unsigned long;
using ino_t = unsigned long;
#include "third_party/zygisk.hpp"

extern "C" int open(const char *, int, ...);
extern "C" long read(int, void *, unsigned long);
extern "C" int close(int);

namespace {
constexpr char marker[] = "/system/fonts/HFC-logical-weights";
constexpr char signature[] = "hyperfont-logical-weights-v1\n";

bool enabled() {
    int fd = open(marker, 0);
    if (fd < 0) return false;
    char bytes[sizeof(signature)]{};
    long size = read(fd, bytes, sizeof(bytes));
    close(fd);
    if (size != sizeof(signature) - 1) return false;
    for (unsigned i = 0; i < sizeof(signature) - 1; ++i)
        if (bytes[i] != signature[i]) return false;
    return true;
}

// Only the MIUI variable-font selector is changed in this forked process.
// No system property, optimization setting, font weight table or app method is hooked.
// The framework can then refresh its own theme_webview selector links. The
// engine must mount/verify both physical XML variants independently of those links.
int apply(JNIEnv *env) {
    if (env->PushLocalFrame(24) < 0) { env->ExceptionClear(); return -1; }
    int result = -1;
    do {
        jclass settings = env->FindClass("miui/util/font/FontSettings");
        if (!settings || env->ExceptionCheck()) { result = 0; break; }
        jfieldID flag = env->GetStaticFieldID(settings, "HAS_MIUI_VAR_FONT", "Z");
        if (!flag || env->ExceptionCheck()) break;
        jclass uc = env->FindClass("jdk/internal/misc/Unsafe");
        if (!uc || env->ExceptionCheck()) break;
        jfieldID singleton = env->GetStaticFieldID(uc, "theUnsafe", "Ljdk/internal/misc/Unsafe;");
        if (!singleton || env->ExceptionCheck()) break;
        jobject unsafe = env->GetStaticObjectField(uc, singleton);
        if (!unsafe || env->ExceptionCheck()) break;
        jmethodID baseFn = env->GetMethodID(uc, "staticFieldBase", "(Ljava/lang/reflect/Field;)Ljava/lang/Object;");
        if (!baseFn || env->ExceptionCheck()) break;
        jmethodID offsetFn = env->GetMethodID(uc, "staticFieldOffset", "(Ljava/lang/reflect/Field;)J");
        if (!offsetFn || env->ExceptionCheck()) break;
        jmethodID writeFn = env->GetMethodID(uc, "putBooleanVolatile", "(Ljava/lang/Object;JZ)V");
        if (!writeFn || env->ExceptionCheck()) break;
        jobject field = env->ToReflectedField(settings, flag, JNI_TRUE);
        if (!field || env->ExceptionCheck()) break;
        jobject base = env->CallObjectMethod(unsafe, baseFn, field);
        if (!base || env->ExceptionCheck()) break;
        jlong offset = env->CallLongMethod(unsafe, offsetFn, field);
        if (env->ExceptionCheck()) break;

        jclass utils = env->FindClass("miui/util/TypefaceUtils");
        if (!utils || env->ExceptionCheck()) break;
        jclass reflection = env->FindClass("miui/util/font/TypefaceReflectionHelper");
        if (!reflection || env->ExceptionCheck()) break;
        jmethodID initialized = env->GetStaticMethodID(reflection, "checkSystemTypefaceFullyInitialized", "()Z");
        if (!initialized || env->ExceptionCheck()) break;
        jfieldID manager = env->GetStaticFieldID(utils, "sDefManager", "Lmiui/util/font/DefaultFontManager;");
        if (!manager || env->ExceptionCheck()) break;
        jmethodID reload = env->GetStaticMethodID(utils, "loadFontManager", "(Lmiui/util/font/IFontManager;Z)V");
        if (!reload || env->ExceptionCheck()) break;
        jobject defaultManager = env->GetStaticObjectField(utils, manager);
        if (!defaultManager || env->ExceptionCheck()) break;
        jboolean prior = env->GetStaticBooleanField(settings, flag);
        env->CallVoidMethod(unsafe, writeFn, base, offset, JNI_FALSE);
        if (env->ExceptionCheck()) break;
        jboolean ready = env->CallStaticBooleanMethod(reflection, initialized);
        if (ready && !env->ExceptionCheck())
            env->CallStaticVoidMethod(utils, reload, defaultManager, JNI_TRUE);
        if (env->ExceptionCheck()) {
            env->ExceptionClear();
            env->CallVoidMethod(unsafe, writeFn, base, offset, prior);
            break;
        }
        result = env->GetStaticBooleanField(settings, flag) == JNI_FALSE ? 1 : -1;
    } while (false);
    if (env->ExceptionCheck()) env->ExceptionClear();
    env->PopLocalFrame(nullptr);
    return result;
}

class HyperFontCompat final : public zygisk::ModuleBase {
    zygisk::Api *api_{};
    JNIEnv *env_{};
    void specialize() {
        if (enabled()) apply(env_);
        // No method/PLT hooks or native worker survives specialization.
        api_->setOption(zygisk::DLCLOSE_MODULE_LIBRARY);
    }
public:
    void onLoad(zygisk::Api *api, JNIEnv *env) override { api_ = api; env_ = env; }
    void preAppSpecialize(zygisk::AppSpecializeArgs *) override { specialize(); }
    void preServerSpecialize(zygisk::ServerSpecializeArgs *) override { specialize(); }
};
} // namespace

REGISTER_ZYGISK_MODULE(HyperFontCompat)

#ifdef HFC_DIAGNOSTICS
extern "C" JNIEXPORT jint JNICALL Java_local_hyperfont_FontCompatProbe_apply(JNIEnv *env, jclass) {
    return apply(env);
}
#endif
